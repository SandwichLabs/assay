package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/engine"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/watches"
)

// The checks against a real DuckDB file through the real read-only runner are
// in cmd/watch_test.go (the workspace package cannot import the hub). These
// tests drive CheckWatches with a scripted runner for what a database will not
// produce on demand: a connection that is down, a statement that never ends,
// every result shape.

type scriptedRunner struct {
	rs        *engine.ResultSet
	truncated bool
	err       error
	// onRun is called for each run; it may block until ctx ends.
	onRun func(ctx context.Context, connection, sql string, params map[string]any, maxRows int) (*engine.ResultSet, bool, error)
	calls []runCall
}

type runCall struct {
	connection, sql string
	params          map[string]any
	maxRows         int
}

func (s *scriptedRunner) RunReadOnlySQLIn(ctx context.Context, connection, sql string, params map[string]any, maxRows int) (*engine.ResultSet, bool, error) {
	s.calls = append(s.calls, runCall{connection, sql, params, maxRows})
	if s.onRun != nil {
		return s.onRun(ctx, connection, sql, params, maxRows)
	}
	return s.rs, s.truncated, s.err
}

func (s *scriptedRunner) VetReadOnlySQLIn(context.Context, string, string, map[string]any) error {
	return nil
}

type connDown struct{ msg string }

func (e connDown) Error() string      { return e.msg }
func (connDown) ConnectFailure() bool { return true }
func scalar(name, typ string, v any) *engine.ResultSet {
	return &engine.ResultSet{Columns: []engine.Column{{Name: name, Type: typ}}, Rows: [][]any{{v}}}
}

func rowsOf(n int) *engine.ResultSet {
	rs := &engine.ResultSet{Columns: []engine.Column{{Name: "id", Type: "INTEGER"}, {Name: "who", Type: "VARCHAR"}}}
	for i := 0; i < n; i++ {
		rs.Rows = append(rs.Rows, []any{int64(i), fmt.Sprintf("person %d", i)})
	}
	return rs
}

func putWatch(t *testing.T, ws Workspace, name, doc string) {
	t.Helper()
	_, err := ws.Watches().PutSource(context.Background(), name, []byte(doc))
	require.NoError(t, err)
}

func watchWorkspace(t *testing.T) Workspace {
	t.Helper()
	ws, _ := newWorkspace(t)
	ctx := context.Background()
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "stuck", Title: "Stuck", SQL: "SELECT id, who FROM orders WHERE age > $days",
		Parameters: []queries.Param{{Name: "days", Type: "integer", Default: "1"}}}))
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "remote_q", Title: "Remote", SQL: "SELECT 1", Connection: "warehouse"}))
	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "base", Title: "Base", SQL: "SELECT region, amount FROM sales", Connection: "warehouse"}))
	m, err := semantics.DecodeMetric("revenue", []byte("version: 1\nname: revenue\nquery: base\nmeasure: sum(amount)\n"))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	return ws
}

func check(t *testing.T, ws Workspace, r ReadOnlyRunner, names ...string) []WatchResult {
	t.Helper()
	res, err := CheckWatches(context.Background(), ws, names, r, time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return res
}

func TestCheckWatches_Thresholds(t *testing.T) {
	cases := []struct {
		name   string
		when   string
		value  any
		typ    string
		status watches.Status
		reason string
	}{
		{"above fires", "above: 1000", int64(1240), "BIGINT", watches.StatusAlert, "revenue is 1,240, above 1,000"},
		{"above holds at the line", "above: 1000", int64(1000), "BIGINT", watches.StatusOK, "revenue is 1,000; alerts above 1,000"},
		{"below fires", "below: 0.95", 0.5, "DOUBLE", watches.StatusAlert, "revenue is 0.5, below 0.95"},
		{"below holds", "below: 0.95", 0.95, "DOUBLE", watches.StatusOK, "alerts below 0.95"},
		{"outside fires low", "outside: [10, 90]", int64(5), "INTEGER", watches.StatusAlert, "revenue is 5, below 10"},
		{"outside fires high", "outside: [10, 90]", int64(91), "INTEGER", watches.StatusAlert, "revenue is 91, above 90"},
		{"outside holds at an end", "outside: [10, 90]", int64(10), "INTEGER", watches.StatusOK, "alerts outside 10 to 90"},
		{"outside holds inside", "outside: [10, 90]", int64(50), "INTEGER", watches.StatusOK, "revenue is 50"},
		{"decimal as a json number", "above: 100", json.Number("1234.50"), "DECIMAL(12,2)", watches.StatusAlert, "revenue is 1,234.5, above 100"},
		{"big integer as text", "above: 1", "12345678901234567890", "HUGEINT", watches.StatusAlert, "above 1"},
		{"negative", "below: 0", int64(-3), "INTEGER", watches.StatusAlert, "revenue is -3, below 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := watchWorkspace(t)
			putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {"+c.when+"}\n")
			r := &scriptedRunner{rs: scalar("revenue", c.typ, c.value)}
			res := check(t, ws, r)
			require.Len(t, res, 1)
			assert.Equal(t, c.status, res[0].Status, res[0].Reason)
			assert.Contains(t, res[0].Reason, c.reason)
			require.NotNil(t, res[0].Value)
			assert.Nil(t, res[0].Rows)
			require.Len(t, r.calls, 1)
			assert.Equal(t, watchValueRows, r.calls[0].maxRows, "a threshold check reads a few rows")
		})
	}
}

func TestCheckWatches_ThresholdShapesAreErrors(t *testing.T) {
	multi := &engine.ResultSet{Columns: []engine.Column{{Name: "a", Type: "INTEGER"}, {Name: "b", Type: "INTEGER"}}, Rows: [][]any{{int64(1), int64(2)}}}
	withValue := &engine.ResultSet{Columns: []engine.Column{{Name: "label", Type: "VARCHAR"}, {Name: "Value", Type: "INTEGER"}}, Rows: [][]any{{"x", int64(2000)}}}
	cases := []struct {
		name      string
		rs        *engine.ResultSet
		truncated bool
		status    watches.Status
		reason    string
	}{
		{"no rows", &engine.ResultSet{Columns: []engine.Column{{Name: "n", Type: "INTEGER"}}}, false, watches.StatusError, "returned no rows"},
		{"two rows", rowsOf(2), false, watches.StatusError, "returned 2 rows"},
		{"more than the read", rowsOf(5), true, watches.StatusError, "more than 5 rows"},
		{"two columns, none called value", multi, false, watches.StatusError, "returned 2 columns (a, b)"},
		{"a text column", scalar("name", "VARCHAR", "acme"), false, watches.StatusError, `column "name" is VARCHAR, not a number`},
		{"a null", scalar("n", "INTEGER", nil), false, watches.StatusError, `column "n" is NULL`},
		{"nan", scalar("n", "DOUBLE", "nan"), false, watches.StatusError, "not a finite number"},
		{"a column called value is read", withValue, false, watches.StatusAlert, "is 2,000, above"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := watchWorkspace(t)
			putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {above: 1500}\n")
			res := check(t, ws, &scriptedRunner{rs: c.rs, truncated: c.truncated})
			require.Len(t, res, 1)
			assert.Equal(t, c.status, res[0].Status, res[0].Reason)
			assert.Contains(t, res[0].Reason, c.reason)
			if c.status == watches.StatusError {
				assert.Contains(t, res[0].Reason, "one row and one numeric column", "the error names the shape it wanted")
				assert.Nil(t, res[0].Value, "a wrong shape is never an alert")
			}
		})
	}
}

func TestCheckWatches_Rows(t *testing.T) {
	cases := []struct {
		name      string
		rows      string
		rs        *engine.ResultSet
		truncated bool
		status    watches.Status
		reason    string
		capped    bool
	}{
		{"any with rows", "any", rowsOf(3), false, watches.StatusAlert, "returned 3 rows", false},
		{"any with one row", "any", rowsOf(1), false, watches.StatusAlert, "returned 1 row", false},
		{"any with none", "any", rowsOf(0), false, watches.StatusOK, "returned no rows", false},
		{"none with rows", "none", rowsOf(2), false, watches.StatusOK, "returned 2 rows", false},
		{"none with none", "none", rowsOf(0), false, watches.StatusAlert, "returned no rows", false},
		{"any at the read limit", "any", rowsOf(1001), false, watches.StatusAlert, "returned 1,000+ rows", true},
		{"any past the read limit", "any", rowsOf(1001), true, watches.StatusAlert, "returned 1,000+ rows", true},
		{"exactly the shown limit", "any", rowsOf(1000), false, watches.StatusAlert, "returned 1,000 rows", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := watchWorkspace(t)
			putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: "+c.rows+"}\n")
			r := &scriptedRunner{rs: c.rs, truncated: c.truncated}
			res := check(t, ws, r)
			require.Len(t, res, 1)
			assert.Equal(t, c.status, res[0].Status, res[0].Reason)
			assert.Contains(t, res[0].Reason, c.reason)
			require.NotNil(t, res[0].Rows)
			assert.Equal(t, len(c.rs.Rows), *res[0].Rows)
			assert.Equal(t, c.capped, res[0].RowsCapped)
			assert.Nil(t, res[0].Value)
			assert.Equal(t, 1001, r.calls[0].maxRows, "a rows check reads at most 1,001 rows")
		})
	}
}

func TestCheckWatches_TheSampleIsCappedAndStaysOutOfTheHistory(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: any}\n")
	wide := &engine.ResultSet{Columns: []engine.Column{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}}}
	for i := 0; i < 50; i++ {
		wide.Rows = append(wide.Rows, []any{fmt.Sprintf("customer %d", i), strings.Repeat("long", 100), nil, int64(i), "hidden fifth column"})
	}
	res := check(t, ws, &scriptedRunner{rs: wide})
	require.Len(t, res, 1)
	s := res[0].Sample
	require.NotNil(t, s)
	assert.Equal(t, []string{"a", "b", "c", "d"}, s.Columns, "the first columns")
	assert.LessOrEqual(t, len(s.Rows), watchSampleRows, "the first rows")
	assert.True(t, s.Capped)
	total := 0
	for _, row := range s.Rows {
		require.Len(t, row, 4)
		assert.LessOrEqual(t, len([]rune(row[1])), watchSampleCell)
		assert.Equal(t, "NULL", row[2])
		for _, c := range row {
			total += len(c)
		}
	}
	assert.LessOrEqual(t, total, watchSampleBytes, "capped in bytes")

	// What the history kept is the count and the reason, not a cell.
	entries, err := ws.WatchHistory().Entries(context.Background(), "w")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	raw, err := json.Marshal(entries[0])
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "customer")
	assert.NotContains(t, string(raw), "hidden fifth column")

	// An ok check has no sample, and neither does a threshold.
	ok := check(t, ws, &scriptedRunner{rs: rowsOf(0)})
	assert.Nil(t, ok[0].Sample)
}

func TestCheckWatches_SampleCellsShowControlCharacters(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: any}\n")
	rs := &engine.ResultSet{Columns: []engine.Column{{Name: "who\x1b[2J"}}, Rows: [][]any{{"evil\x1b]0;pwned\x07"}}}
	res := check(t, ws, &scriptedRunner{rs: rs})
	require.NotNil(t, res[0].Sample)
	assert.NotContains(t, res[0].Sample.Rows[0][0], "\x1b")
	assert.NotContains(t, res[0].Sample.Columns[0], "\x1b")
	assert.Contains(t, res[0].Sample.Rows[0][0], "<U+001B>")
}

func TestCheckWatches_SavedQueryParamsAreBoundNotInterpolated(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question:\n  query: stuck\n  params: {days: 7}\nwhen: {rows: any}\n")
	r := &scriptedRunner{rs: rowsOf(0)}
	check(t, ws, r)
	require.Len(t, r.calls, 1)
	assert.Equal(t, "SELECT id, who FROM orders WHERE age > $days", r.calls[0].sql, "the statement is the saved query's own, unchanged")
	assert.Equal(t, map[string]any{"days": int64(7)}, r.calls[0].params, "the value travels as a typed bound parameter")
	assert.Equal(t, "", r.calls[0].connection, "the saved query's own connection")

	// The declared default applies when the watch gives none.
	putWatch2 := func(name, doc string) {
		_, err := ws.Watches().PutSource(context.Background(), name, []byte(doc))
		require.NoError(t, err)
	}
	putWatch2("w2", "question: {query: stuck}\nwhen: {rows: any}\n")
	r2 := &scriptedRunner{rs: rowsOf(0)}
	check(t, ws, r2, "w2")
	assert.Equal(t, map[string]any{"days": int64(1)}, r2.calls[0].params)

	// Text that is not an integer is refused before a statement exists.
	putWatch2("w3", "question:\n  query: stuck\n  params: {days: \"1; DROP TABLE orders\"}\nwhen: {rows: any}\n")
	r3 := &scriptedRunner{rs: rowsOf(0)}
	res := check(t, ws, r3, "w3")
	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.Contains(t, res[0].Reason, "whole number")
	assert.Empty(t, r3.calls, "nothing ran")

	// An undeclared name is refused, naming the declared ones.
	putWatch2("w4", "question:\n  query: stuck\n  params: {limit: 5}\nwhen: {rows: any}\n")
	res = check(t, ws, &scriptedRunner{rs: rowsOf(0)}, "w4")
	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.Contains(t, res[0].Reason, "no parameter named")
	assert.Contains(t, res[0].Reason, "days")
}

func TestCheckWatches_ASavedQueryOnAnotherConnection(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: remote_q}\nwhen: {above: 5}\n")
	r := &scriptedRunner{rs: scalar("n", "INTEGER", int64(1))}
	check(t, ws, r)
	assert.Equal(t, "warehouse", r.calls[0].connection)
}

func TestCheckWatches_AMetricRunsAtItsTotalOnItsQueriesConnection(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {above: 5}\n")
	putWatch(t, ws, "w2", "question: {metric: revenue, connection: other}\nwhen: {above: 5}\n")
	r := &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(1))}
	check(t, ws, r, "w", "w2")
	require.Len(t, r.calls, 2)
	assert.Equal(t, "SELECT * FROM metric('revenue')", r.calls[0].sql, "the grain total, written by MetricQuery")
	assert.Equal(t, "warehouse", r.calls[0].connection)
	assert.Equal(t, "other", r.calls[1].connection, "the watch may name the connection")
}

func TestCheckWatches_ADownConnectionIsAnErrorNamingIt(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: remote_q}\nwhen: {below: 1}\n")
	res := check(t, ws, &scriptedRunner{err: connDown{"the remote service refused the token"}})
	assert.Equal(t, watches.StatusError, res[0].Status, "never an alert")
	assert.Contains(t, res[0].Reason, "could not connect to warehouse: the remote service refused the token")
	assert.Nil(t, res[0].Value)

	// A saved query on the default connection says so.
	putWatch(t, ws, "d", "question: {query: stuck}\nwhen: {rows: any}\n")
	res = check(t, ws, &scriptedRunner{err: connDown{"disk gone"}}, "d")
	assert.Contains(t, res[0].Reason, "could not connect to "+ws.DefaultConnection())

	// Any other failure is not dressed up as a connection problem.
	res = check(t, ws, &scriptedRunner{err: errors.New("Catalog Error: table nope does not exist")}, "d")
	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.True(t, strings.HasPrefix(res[0].Reason, "could not run: Catalog Error"), res[0].Reason)
}

func TestCheckWatches_APolicyRefusalIsAnError(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: any}\n")
	res := check(t, ws, &scriptedRunner{err: errors.New("live bindings are read-only queries (no writes, and files only inside data/)")})
	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.Contains(t, res[0].Reason, "read-only")
}

func TestCheckWatches_OneBadWatchDoesNotStopTheOthers(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "a_ok", "question: {metric: revenue}\nwhen: {above: 100}\n")
	putWatch(t, ws, "b_missing_query", "question: {query: no_such_query}\nwhen: {rows: any}\n")
	putWatch(t, ws, "c_missing_metric", "question: {metric: nope}\nwhen: {above: 1}\n")
	putWatch(t, ws, "d_fires", "question: {metric: revenue}\nwhen: {below: 100}\n")
	root := ws.Root()
	require.NoError(t, os.WriteFile(filepath.Join(root, "watches", "e_broken.yaml"), []byte("question: {query: q}\n"), 0o644))

	res := check(t, ws, &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(50))})
	require.Len(t, res, 5)
	by := map[string]WatchResult{}
	for _, r := range res {
		by[r.Watch] = r
	}
	assert.Equal(t, watches.StatusOK, by["a_ok"].Status)
	assert.Contains(t, by["b_missing_query"].Reason, `there is no saved query "no_such_query"`)
	assert.Equal(t, watches.StatusError, by["b_missing_query"].Status)
	assert.Equal(t, watches.StatusError, by["c_missing_metric"].Status)
	assert.Equal(t, watches.StatusAlert, by["d_fires"].Status, "a watch after the failures still ran")
	assert.Equal(t, watches.StatusError, by["e_broken"].Status)
	assert.Contains(t, by["e_broken"].Reason, "the file cannot be read")
	assert.Contains(t, by["e_broken"].Reason, "when needs exactly one condition")

	// Every outcome, including the errors, is in the history.
	all, err := ws.WatchHistory().Entries(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, all, 5)
}

func TestCheckWatches_NamesAndUnknownNames(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "a", "question: {metric: revenue}\nwhen: {above: 100}\n")
	putWatch(t, ws, "b", "question: {metric: revenue}\nwhen: {above: 100}\n")
	r := &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(1))}
	res := check(t, ws, r, "b", "b", "a")
	require.Len(t, res, 2, "a repeated name is checked once")
	assert.Equal(t, "b", res[0].Watch, "in the order asked")

	r2 := &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(1))}
	_, err := CheckWatches(context.Background(), ws, []string{"a", "nope"}, r2, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no watch named "nope"`)
	assert.Empty(t, r2.calls, "nothing ran before the typo was found")

	none, err := CheckWatches(context.Background(), watchWorkspaceEmpty(t), nil, r2, time.Now())
	require.NoError(t, err)
	assert.Empty(t, none)
}

func watchWorkspaceEmpty(t *testing.T) Workspace {
	t.Helper()
	ws, _ := newWorkspace(t)
	return ws
}

func TestCheckWatches_EachStatementIsBounded(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: any}\n")
	var deadline time.Time
	r := &scriptedRunner{onRun: func(ctx context.Context, _, _ string, _ map[string]any, _ int) (*engine.ResultSet, bool, error) {
		deadline, _ = ctx.Deadline()
		return rowsOf(0), false, nil
	}}
	start := time.Now()
	check(t, ws, r)
	require.False(t, deadline.IsZero(), "the statement has a deadline")
	assert.InDelta(t, CheckStatementTimeout.Seconds(), deadline.Sub(start).Seconds(), 5)

	// A statement that hits it is an error saying so.
	r2 := &scriptedRunner{err: context.DeadlineExceeded}
	res := check(t, ws, r2)
	assert.Equal(t, watches.StatusError, res[0].Status)
	assert.Contains(t, res[0].Reason, "did not finish within")
}

func TestCheckWatches_CancelStopsAndRecordsNothingForTheInterruptedWatch(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "a_done", "question: {query: stuck}\nwhen: {rows: any}\n")
	putWatch(t, ws, "b_slow", "question: {query: stuck}\nwhen: {rows: any}\n")
	putWatch(t, ws, "c_never", "question: {query: stuck}\nwhen: {rows: any}\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := 0
	r := &scriptedRunner{onRun: func(ctx context.Context, _, _ string, _ map[string]any, _ int) (*engine.ResultSet, bool, error) {
		n++
		if n == 2 {
			cancel()
			<-ctx.Done()
			return nil, false, ctx.Err()
		}
		return rowsOf(0), false, nil
	}}
	res, err := CheckWatches(ctx, ws, nil, r, time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	require.Len(t, res, 2, "the third never started")
	assert.Equal(t, watches.StatusOK, res[0].Status)
	assert.Equal(t, watches.StatusError, res[1].Status)
	assert.Contains(t, res[1].Reason, "canceled")
	all, herr := ws.WatchHistory().Entries(context.Background(), "")
	require.NoError(t, herr)
	require.Len(t, all, 1, "an interrupted check says nothing about the watch")
	assert.Equal(t, "a_done", all[0].Watch)
}

func TestCheckWatches_AHistoryFailureDoesNotHideTheResult(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {above: 1}\n")
	wsx := &failingHistory{Workspace: ws}
	res, err := CheckWatches(context.Background(), wsx, nil, &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(5))}, time.Now())
	require.NoError(t, err)
	assert.Equal(t, watches.StatusAlert, res[0].Status)
	assert.Contains(t, res[0].HistoryError, "disk full")
}

type failingHistory struct{ Workspace }

func (f *failingHistory) WatchHistory() watches.History {
	return brokenHistory{}
}

// brokenHistory is a history that cannot be written.
type brokenHistory struct{}

func (brokenHistory) Append(context.Context, watches.Entry) error {
	return errors.New("disk full")
}

func (brokenHistory) Entries(context.Context, string) ([]watches.Entry, error) {
	return nil, errors.New("disk full")
}

func TestCheckWatches_ReasonsAreOneBoundedSafeLine(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {query: stuck}\nwhen: {rows: any}\n")
	nasty := "Conversion Error: 'Alice\x1b[31m'\npassword=hunter2 " + strings.Repeat("x", 1000)
	res := check(t, ws, &scriptedRunner{err: errors.New(nasty)})
	assert.LessOrEqual(t, len([]rune(res[0].Reason)), watches.MaxReasonRunes)
	assert.NotContains(t, res[0].Reason, "\x1b")
	assert.NotContains(t, res[0].Reason, "\n")
	assert.NotContains(t, res[0].Reason, "hunter2", "credential-shaped text is redacted")
}

func TestCheckWatches_RecordsTheRevisionTimeAndDuration(t *testing.T) {
	ws := watchWorkspace(t)
	putWatch(t, ws, "w", "question: {metric: revenue}\nwhen: {above: 1}\n")
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	_, err := CheckWatches(context.Background(), ws, nil, &scriptedRunner{rs: scalar("revenue", "BIGINT", int64(5))}, now)
	require.NoError(t, err)
	got, err := ws.Watches().Get(context.Background(), "w")
	require.NoError(t, err)
	all, err := ws.WatchHistory().Entries(context.Background(), "w")
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, got.Rev, all[0].Rev, "the entry says which version of the file was checked")
	assert.True(t, all[0].At.Equal(now))
	require.NotNil(t, all[0].Value)
	assert.EqualValues(t, 5, *all[0].Value)
}
