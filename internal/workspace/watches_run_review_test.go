package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/watches"
)

type refusedErr struct{ msg string }

func (e refusedErr) Error() string     { return e.msg }
func (refusedErr) PolicyRefused() bool { return true }

func historyBytes(t *testing.T, ws Workspace) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(ws.Root(), filepath.FromSlash(watches.HistoryPath)))
	require.NoError(t, err)
	return string(b)
}

// A database error can quote a value of the row it choked on. The report shows
// it; the history keeps only the class.
func TestCheckWatches_ADatabaseErrorQuotingAValueNeverReachesTheHistory(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {above: 1}\n")
	dbErr := errors.New("query execution failed: failed to execute query on duckdb: Conversion Error: Could not convert string 'alice@example.com' to INT32\n\nLINE 1: SELECT CAST(email AS INT) FROM customers\n                         ^")
	res := check(t, ws, &scriptedRunner{err: dbErr})

	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.Equal(t, "could not run: Conversion Error: Could not convert string 'alice@example.com' to INT32", res[0].Reason,
		"the report shows the message without the wrapper chain and without the LINE excerpt")

	entries, err := ws.WatchHistory().Entries(context.Background(), "w")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "could not run: Conversion Error", entries[0].Reason)
	raw := historyBytes(t, ws)
	assert.NotContains(t, raw, "alice")
	assert.NotContains(t, raw, "example.com")
	assert.NotContains(t, raw, "CAST")

	// A message that is not in a class shape is "database error", never itself.
	res = check(t, ws, &scriptedRunner{err: errors.New("bob@example.com exploded")})
	assert.Contains(t, res[0].Reason, "bob@example.com", "shown")
	assert.NotContains(t, historyBytes(t, ws), "bob@example.com")
	assert.Contains(t, historyBytes(t, ws), "could not run: database error")
}

func TestCheckWatches_AConnectFailureStoresOnlyTheConnection(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: remote_q}\nwhen: {below: 1}\n")
	res := check(t, ws, &scriptedRunner{err: connDown{"dial tcp 10.1.2.3:5432: refused for user carol"}})
	assert.Contains(t, res[0].Reason, "could not connect to warehouse: dial tcp")
	entries, err := ws.WatchHistory().Entries(context.Background(), "w")
	require.NoError(t, err)
	assert.Equal(t, "could not connect to warehouse", entries[0].Reason)
}

// Alert and ok reasons hold only our own formatting of the value or the count.
func TestCheckWatches_AlertReasonsHoldOnlyOurOwnFormatting(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "t", "question: {metric: revenue}\nwhen: {above: 1}\n")
	putWatch(t, ws, "r", "question: {query: stuck}\nwhen: {rows: any}\n")
	check(t, ws, &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(7))}, "t")
	check(t, ws, &scriptedRunner{rs: rowsOf(3)}, "r")
	all, err := ws.WatchHistory().Entries(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "revenue is 7, above 1", all[0].Reason)
	assert.Equal(t, "stuck returned 3 rows", all[1].Reason)
	assert.NotContains(t, historyBytes(t, ws), "person ", "no cell of the rows")
}

func TestCheckWatches_APolicyRefusalHasTheWatchWording(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: any}\n")
	res := check(t, ws, &scriptedRunner{err: refusedErr{"live bindings are read-only queries over this workspace's connections"}})
	assert.Equal(t, "refused: a watch may only read (no writes, COPY, ATTACH or SET; files only inside data/)", res[0].Reason)
	assert.NotContains(t, res[0].Reason, "live bindings")
	// Wrapped, it is still found.
	res = check(t, ws, &scriptedRunner{err: errors.Join(errors.New("query execution failed"), refusedErr{"x"})})
	assert.Contains(t, res[0].Reason, "refused:")
	// Text alone is not a refusal.
	res = check(t, ws, &scriptedRunner{err: errors.New("live bindings are read-only queries")})
	assert.True(t, strings.HasPrefix(res[0].Reason, "could not run:"))
}

func TestCheckWatches_ACancelInTheOnlyOrLastWatchIsStillReported(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "only", "question: {query: stuck}\nwhen: {rows: any}\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &scriptedRunner{onRun: func(ctx context.Context, _, _ string, _ map[string]any, _ int) (*engine.ResultSet, bool, error) {
		cancel()
		<-ctx.Done()
		return nil, false, ctx.Err()
	}}
	res, err := CheckWatches(ctx, ws, nil, r, time.Now())
	require.Error(t, err, "a cancel during the only watch is not a clean finish")
	assert.ErrorIs(t, err, context.Canceled)
	require.Len(t, res, 1)
	assert.Contains(t, res[0].Reason, "canceled")
	all, herr := ws.WatchHistory().Entries(context.Background(), "")
	require.NoError(t, herr)
	assert.Empty(t, all, "nothing is recorded for the interrupted watch")
}

func TestCheckWatches_ShapesAndMessages(t *testing.T) {
	twoValues := &engine.ResultSet{Columns: []engine.Column{{Name: "value", Type: "INTEGER"}, {Name: "VALUE", Type: "INTEGER"}}, Rows: [][]any{{int64(1), int64(2)}}}
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {above: 1}\n")

	res := check(t, ws, &scriptedRunner{rs: twoValues})
	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.Contains(t, res[0].Reason, "2 columns named value")

	res = check(t, ws, &scriptedRunner{rs: scalar("n", "INTEGER", nil)})
	assert.Equal(t, `column "n" is NULL`, res[0].Reason, "no shape lecture for a NULL")
}

func TestCheckWatches_PlanningMessages(t *testing.T) {
	ws := watchWorkspace(t)
	ctx := context.Background()
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "gone_q", Title: "Gone", SQL: "SELECT 1", Connection: "nowhere"}))
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "needs", Title: "Needs", SQL: "SELECT $region AS r",
		Parameters: []queries.Param{{Name: "region", Type: "text", Required: true}}}))
	putWatch(t, ws, "unknown_conn", "question: {query: gone_q}\nwhen: {rows: any}\n")
	putWatch(t, ws, "needs_param", "question: {query: needs}\nwhen: {rows: any}\n")
	putWatch(t, ws, "has_param", "question:\n  query: needs\n  params: {region: east}\nwhen: {rows: any}\n")
	r := &scriptedRunner{rs: rowsOf(0)}
	res := check(t, ws, r, "unknown_conn", "needs_param", "has_param")
	assert.Equal(t, `saved query "gone_q" runs on connection "nowhere", which this workspace does not define (slaw conn list)`, res[0].Reason)
	assert.Equal(t, `saved query "needs" needs a value for parameter "region": add question.params.region to the watch`, res[1].Reason)
	assert.Equal(t, watches.StatusOK, res[2].Status)
	assert.Len(t, r.calls, 1, "only the complete watch ran")
}

func TestCheckWatches_ValuesAreComparedExactly(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {above: 999999999999999}\n")
	// A double rounds this to the threshold; the decimal does not.
	res := check(t, ws, &scriptedRunner{rs: scalar("revenue", "DECIMAL(25,7)", json.Number("999999999999999.0000001"))})
	assert.Equal(t, watches.StatusAlert, res[0].Status, res[0].Reason)
	res = check(t, ws, &scriptedRunner{rs: scalar("revenue", "DECIMAL(25,7)", json.Number("999999999999999.0000000"))})
	assert.Equal(t, watches.StatusOK, res[0].Status, res[0].Reason)

	// An integer too big for a double is shown in full and compared exactly.
	res = check(t, ws, &scriptedRunner{rs: scalar("revenue", "HUGEINT", "123456789012345678901234567890")})
	assert.Equal(t, watches.StatusAlert, res[0].Status)
	assert.Contains(t, res[0].Reason, "123,456,789,012,345,678,901,234,567,890")

	// A double is compared as a double: 0.1 is not above 0.1.
	putWatch(t, ws, "d", "question: {metric: revenue}\nwhen: {above: 0.1}\n")
	res = check(t, ws, &scriptedRunner{rs: scalar("revenue", "DOUBLE", 0.1)}, "d")
	assert.Equal(t, watches.StatusOK, res[0].Status, res[0].Reason)
}

func TestCheckWatches_ASmallValueIsNeverShownAsZero(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {below: 0.000001}\n")
	res := check(t, ws, &scriptedRunner{rs: scalar("revenue", "DOUBLE", 0.0000004)})
	assert.Equal(t, watches.StatusAlert, res[0].Status)
	assert.Equal(t, "revenue is 0.0000004, below 0.000001", res[0].Reason)
	putWatch(t, ws, "z", "question: {metric: revenue}\nwhen: {above: 0.0000001}\n")
	res = check(t, ws, &scriptedRunner{rs: scalar("revenue", "DOUBLE", 0.0000004)}, "z")
	assert.Equal(t, "revenue is 0.0000004, above 0.0000001", res[0].Reason)
}
