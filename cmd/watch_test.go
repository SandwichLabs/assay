package cmd

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/bundle"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// These tests run `slaw watch` against a real DuckDB database through the real
// read-only runner (the agent policy's path), the way a person would.

const (
	wMetric = "version: 1\nname: revenue\nquery: revenue_base\nmeasure: sum(amount)\ndimensions: [region]\n"
	wStuck  = "description: Orders stuck in pending\nquestion:\n  query: stuck_orders\n  params: {days: 1}\nwhen:\n  rows: any\n"
)

func writeIn(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

// watchWorkspaceCLI is a workspace with real tables, saved queries (one with a
// parameter), a metric over a saved query, and no watches yet.
func watchWorkspaceCLI(t *testing.T) string {
	t.Helper()
	root := newWS(t)
	for _, q := range []string{
		"CREATE TABLE orders AS SELECT * FROM (VALUES (1,'acme','pending',3),(2,'globex','pending',2),(3,'initech','shipped',9),(4,'umbrella','pending',0)) t(id,customer,status,age_days)",
		"CREATE TABLE sales AS SELECT * FROM (VALUES ('east',10),('east',20),('west',5)) t(region,amount)",
	} {
		_, stderr, err := runSlaw("query", "--workspace", root, q)
		require.NoError(t, err, stderr)
	}
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	ctx := context.Background()
	for _, q := range []*queries.Query{
		{ID: "stuck_orders", Title: "Stuck orders", SQL: "SELECT id, customer FROM orders WHERE status = 'pending' AND age_days > $days",
			Parameters: []queries.Param{{Name: "days", Type: "integer", Default: "1"}}},
		{ID: "order_count", Title: "Order count", SQL: "SELECT count(*) AS n FROM orders"},
		{ID: "order_list", Title: "Order list", SQL: "SELECT id, customer FROM orders"},
		{ID: "customer_names", Title: "Names", SQL: "SELECT customer FROM orders"},
		{ID: "revenue_base", Title: "Revenue base", SQL: "SELECT region, amount FROM sales"},
	} {
		require.NoError(t, ws.Queries().Create(ctx, q))
	}
	require.NoError(t, ws.Close())
	writeIn(t, root, "semantics/metrics/revenue.yaml", wMetric)
	return root
}

type jsonResult struct {
	Watch     string   `json:"watch"`
	Status    string   `json:"status"`
	Value     *float64 `json:"value"`
	Rows      *int     `json:"rows"`
	Reason    string   `json:"reason"`
	Question  string   `json:"question"`
	Condition string   `json:"condition"`
	Sample    *struct {
		Columns []string   `json:"columns"`
		Rows    [][]string `json:"rows"`
	} `json:"sample"`
}

func checkJSON(t *testing.T, root string, names ...string) (map[string]jsonResult, int, string) {
	t.Helper()
	args := append([]string{"watch", "check", "--json", "--workspace", root}, names...)
	out, stderr, err := runSlaw(args...)
	var rows []jsonResult
	require.NoError(t, json.Unmarshal([]byte(out), &rows), out+stderr)
	by := map[string]jsonResult{}
	for _, r := range rows {
		by[r.Watch] = r
	}
	return by, exitStatus(err), stderr
}

func TestWatchCheck_ThresholdsOnARealDatabase(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/volume_low.yaml", "question: {query: order_count}\nwhen: {below: 10}\n")
	writeIn(t, root, "watches/volume_high.yaml", "question: {query: order_count}\nwhen: {above: 3}\n")
	writeIn(t, root, "watches/volume_edge.yaml", "question: {query: order_count}\nwhen: {above: 4}\n")
	writeIn(t, root, "watches/volume_in_band.yaml", "question: {query: order_count}\nwhen: {outside: [1, 10]}\n")
	writeIn(t, root, "watches/volume_out_of_band.yaml", "question: {query: order_count}\nwhen: {outside: [10, 20]}\n")
	writeIn(t, root, "watches/revenue_high.yaml", "question: {metric: revenue}\nwhen: {above: 30}\n")
	writeIn(t, root, "watches/revenue_fine.yaml", "question: {metric: revenue}\nwhen: {outside: [0, 100]}\n")

	got, code, stderr := checkJSON(t, root)
	assert.Equal(t, 1, code, stderr)
	want := map[string]string{
		"volume_low": "alert", "volume_high": "alert", "volume_edge": "ok",
		"volume_in_band": "ok", "volume_out_of_band": "alert",
		"revenue_high": "alert", "revenue_fine": "ok",
	}
	for name, status := range want {
		assert.Equal(t, status, got[name].Status, name+": "+got[name].Reason)
	}
	require.NotNil(t, got["volume_low"].Value)
	assert.EqualValues(t, 4, *got["volume_low"].Value)
	assert.Equal(t, "order_count is 4, below 10", got["volume_low"].Reason)
	assert.Equal(t, "query order_count", got["volume_low"].Question)
	assert.Equal(t, "below 10", got["volume_low"].Condition)
	assert.Equal(t, "order_count is 4; alerts above 4", got["volume_edge"].Reason)
	assert.Equal(t, "order_count is 4, below 10", got["volume_out_of_band"].Reason)
	assert.Equal(t, "revenue is 35, above 30", got["revenue_high"].Reason, "the metric ran at its grain total")
	assert.Equal(t, "revenue is 35; alerts outside 0 to 100", got["revenue_fine"].Reason)
}

func TestWatchCheck_RowsAnyAndNone(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/stuck.yaml", wStuck)
	writeIn(t, root, "watches/none_stuck_for_9.yaml", "question:\n  query: stuck_orders\n  params: {days: 9}\nwhen: {rows: any}\n")
	writeIn(t, root, "watches/heartbeat.yaml", "question:\n  query: stuck_orders\n  params: {days: 9}\nwhen: {rows: none}\n")
	writeIn(t, root, "watches/orders_exist.yaml", "question: {query: order_list}\nwhen: {rows: none}\n")

	got, code, stderr := checkJSON(t, root)
	assert.Equal(t, 1, code, stderr)
	assert.Equal(t, "alert", got["stuck"].Status)
	assert.Equal(t, "stuck_orders returned 2 rows", got["stuck"].Reason)
	require.NotNil(t, got["stuck"].Rows)
	assert.Equal(t, 2, *got["stuck"].Rows)
	require.NotNil(t, got["stuck"].Sample)
	assert.Equal(t, []string{"id", "customer"}, got["stuck"].Sample.Columns)
	assert.Equal(t, [][]string{{"1", "acme"}, {"2", "globex"}}, got["stuck"].Sample.Rows)

	assert.Equal(t, "ok", got["none_stuck_for_9"].Status, "the parameter value reached the query")
	assert.Equal(t, "stuck_orders returned no rows", got["none_stuck_for_9"].Reason)
	assert.Equal(t, "alert", got["heartbeat"].Status, "rows none fires on an empty result")
	assert.Equal(t, "ok", got["orders_exist"].Status)
	assert.Nil(t, got["none_stuck_for_9"].Sample)
}

func TestWatchCheck_WrongShapeIsAnErrorNotAnAlert(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/many_rows.yaml", "question: {query: order_list}\nwhen: {above: 1}\n")
	writeIn(t, root, "watches/text_column.yaml", "question: {query: customer_names}\nwhen: {above: 1}\n")
	writeIn(t, root, "watches/two_columns.yaml", "question:\n  query: stuck_orders\n  params: {days: 2}\nwhen: {above: 1}\n")

	got, code, stderr := checkJSON(t, root)
	assert.Equal(t, 2, code, "errors only: nothing fired, so exit 2: "+stderr)
	assert.Equal(t, "error", got["many_rows"].Status)
	assert.Contains(t, got["many_rows"].Reason, "returned 4 rows")
	assert.Contains(t, got["many_rows"].Reason, "one row and one numeric column")
	assert.Equal(t, "error", got["text_column"].Status)
	assert.Contains(t, got["text_column"].Reason, "returned 4 rows")
	assert.Equal(t, "error", got["two_columns"].Status)
	assert.Contains(t, got["two_columns"].Reason, "2 columns (id, customer)")
	for _, r := range got {
		assert.Nil(t, r.Value)
	}
}

func TestWatchCheck_AHostileCloneIsRefusedByThePolicy(t *testing.T) {
	root := watchWorkspaceCLI(t)
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOP-SECRET-VALUE"), 0o644))
	exfil := filepath.Join(secretDir, "exfil.csv")

	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	ctx := context.Background()
	for _, q := range []*queries.Query{
		{ID: "copy_out", Title: "Copy out", SQL: "COPY (SELECT * FROM orders) TO '" + exfil + "' (FORMAT csv)"},
		{ID: "read_file", Title: "Read file", SQL: "SELECT content AS n FROM read_text('" + secret + "')"},
		{ID: "drop_it", Title: "Drop", SQL: "DROP TABLE orders"},
		{ID: "attach_it", Title: "Attach", SQL: "ATTACH '" + filepath.Join(secretDir, "x.duckdb") + "' AS x"},
		{ID: "set_it", Title: "Set", SQL: "SET enable_external_access = true"},
	} {
		require.NoError(t, ws.Queries().Create(ctx, q))
	}
	require.NoError(t, ws.Close())
	for _, n := range []string{"copy_out", "read_file", "drop_it", "attach_it", "set_it"} {
		writeIn(t, root, "watches/"+n+".yaml", "question: {query: "+n+"}\nwhen: {rows: any}\n")
	}
	writeIn(t, root, "watches/fine.yaml", "question: {query: order_count}\nwhen: {below: 10}\n")

	out, stderr, err := runSlaw("watch", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err), "the clean watch fired; the hostile ones are errors")
	assert.NotContains(t, out+stderr, "TOP-SECRET-VALUE")
	for _, n := range []string{"copy_out", "read_file", "drop_it", "attach_it", "set_it"} {
		assert.Regexp(t, `error\s+`+n+`\s+could not run`, out, n)
	}
	assert.Regexp(t, `alert\s+fine\s`, out, "one bad watch does not stop the others")
	assert.NoFileExists(t, exfil, "the COPY never ran")
	_, _, err = runSlaw("query", "--workspace", root, "SELECT count(*) FROM orders")
	assert.NoError(t, err, "the table is still there")

	// Nothing the refused statements tried is in the history either.
	hist, _, err := runSlaw("watch", "history", "--json", "-n", "0", "--workspace", root)
	require.NoError(t, err)
	assert.NotContains(t, hist, "TOP-SECRET-VALUE")
}

func TestWatchCheck_ADownConnectionIsAnErrorNamingIt(t *testing.T) {
	root := watchWorkspaceCLI(t)
	_, stderr, err := runSlaw("conn", "add", "broken", "--type", "postgres", "--host", "127.0.0.1", "--port", "1", "--database", "x", "--sign-in", "env:SLAW_WATCH_TEST_NOPE", "--workspace", root)
	require.NoError(t, err, stderr)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{ID: "remote", Title: "Remote", SQL: "SELECT 1 AS n", Connection: "broken"}))
	require.NoError(t, ws.Close())
	writeIn(t, root, "watches/remote.yaml", "question: {query: remote}\nwhen: {below: 5}\n")

	got, code, _ := checkJSON(t, root)
	assert.Equal(t, 2, code)
	assert.Equal(t, "error", got["remote"].Status, "never an alert")
	assert.Contains(t, got["remote"].Reason, "broken", "the connection is named")
	assert.Nil(t, got["remote"].Value)
}

func TestWatchCheck_OneBadWatchDoesNotStopTheOthers(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/a_missing_query.yaml", "question: {query: not_there}\nwhen: {rows: any}\n")
	writeIn(t, root, "watches/b_broken_file.yaml", "question: {query: order_count}\n")
	writeIn(t, root, "watches/c_bad_param.yaml", "question:\n  query: stuck_orders\n  params: {days: lots}\nwhen: {rows: any}\n")
	writeIn(t, root, "watches/d_fires.yaml", "question: {query: order_count}\nwhen: {below: 10}\n")
	writeIn(t, root, "watches/e_fine.yaml", "question: {query: order_count}\nwhen: {above: 10}\n")

	got, code, stderr := checkJSON(t, root)
	assert.Equal(t, 1, code, "an alert outranks the errors: "+stderr)
	assert.Equal(t, "error", got["a_missing_query"].Status)
	assert.Contains(t, got["a_missing_query"].Reason, `no saved query "not_there"`)
	assert.Equal(t, "error", got["b_broken_file"].Status)
	assert.Contains(t, got["b_broken_file"].Reason, "when needs exactly one condition")
	assert.Equal(t, "error", got["c_bad_param"].Status)
	assert.Contains(t, got["c_bad_param"].Reason, "whole number")
	assert.Equal(t, "alert", got["d_fires"].Status)
	assert.Equal(t, "ok", got["e_fine"].Status)
}

func TestWatchCheck_ExitCodesAndTables(t *testing.T) {
	root := watchWorkspaceCLI(t)

	out, _, err := runSlaw("watch", "check", "--workspace", root)
	require.NoError(t, err)
	assert.Equal(t, 0, exitStatus(err))
	assert.Contains(t, out, "No watches")

	writeIn(t, root, "watches/quiet.yaml", "question: {query: order_count}\nwhen: {above: 10}\n")
	out, _, err = runSlaw("watch", "check", "--workspace", root)
	require.NoError(t, err, "every watch ok is exit 0")
	assert.Regexp(t, `ok\s+quiet\s+order_count is 4; alerts above 10`, out)
	assert.Contains(t, out, "1 ok, 0 alerts, 0 errors")

	writeIn(t, root, "watches/loud.yaml", "question: {query: order_count}\nwhen: {below: 10}\n")
	writeIn(t, root, "watches/oops.yaml", "question: {query: nope}\nwhen: {rows: any}\n")
	out, _, err = runSlaw("watch", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err), "any alert is exit 1")
	assert.Regexp(t, `alert\s+loud\s+order_count is 4, below 10`, out)
	assert.Regexp(t, `error\s+oops\s+there is no saved query "nope"`, out)
	assert.Contains(t, out, "1 ok, 1 alert, 1 error")

	// Naming watches checks just those; an error-only selection exits 2.
	_, _, err = runSlaw("watch", "check", "oops", "--workspace", root)
	assert.Equal(t, 2, exitStatus(err))
	_, _, err = runSlaw("watch", "check", "quiet", "--workspace", root)
	assert.Equal(t, 0, exitStatus(err))

	// Usage and setup problems are 2, never 1.
	for _, args := range [][]string{
		{"check", "nope"},
		{"check", "quiet", "nope"},
		{"check", "--no-such-flag"},
		{"show"},
		{"show", "a", "b"},
		{"show", "nope"},
		{"history", "a", "b"},
		{"history", "--limit", "-1"},
		{"list", "extra"},
	} {
		_, _, err := runSlaw(append(append([]string{"watch"}, args...), "--workspace", root)...)
		require.Error(t, err, args)
		assert.Equal(t, 2, exitStatus(err), args)
	}
	_, _, err = runSlaw("watch", "check", "--workspace", t.TempDir())
	assert.Equal(t, 2, exitStatus(err), "no workspace")
}

func TestWatchCheck_PrintsTheSampleOfAnAlertingRowsCheck(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/stuck.yaml", wStuck)
	out, _, err := runSlaw("watch", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))
	assert.Contains(t, out, "stuck: first 2 rows (id, customer)")
	assert.Contains(t, out, "  1 | acme")
	assert.Contains(t, out, "  2 | globex")
}

func TestWatchListShowAndHistory(t *testing.T) {
	root := watchWorkspaceCLI(t)
	out, _, err := runSlaw("watch", "list", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "No watches")

	writeIn(t, root, "watches/stuck.yaml", wStuck)
	writeIn(t, root, "watches/quiet.yaml", "question: {query: order_count}\nwhen: {above: 10}\n")
	writeIn(t, root, "watches/broken.yaml", "question: {query: order_count}\nbogus: 1\n")

	out, _, err = runSlaw("watch", "list", "--workspace", root)
	require.NoError(t, err)
	assert.Regexp(t, `stuck\s+query stuck_orders\s+any rows\s+never\s+never\s+-`, out)
	assert.Regexp(t, `broken\s+-\s+-\s+never\s+never\s+.*field bogus not found`, out, "a broken file is listed with its error")

	_, _, err = runSlaw("watch", "check", "stuck", "quiet", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))

	out, _, err = runSlaw("watch", "list", "--workspace", root)
	require.NoError(t, err)
	assert.NotRegexp(t, `stuck\s+query stuck_orders\s+any rows\s+never`, out, "last checked is filled in from the history")
	assert.Regexp(t, `quiet\s+query order_count\s+above 10\s+\d{4}-\d\d-\d\d \d\d:\d\d\s+never`, out, "an ok check was checked but never fired")
	assert.Regexp(t, `stuck\s+query stuck_orders\s+any rows\s+\d{4}-\d\d-\d\d \d\d:\d\d\s+\d{4}-\d\d-\d\d \d\d:\d\d`, out, "a fired check has both times")

	out, _, err = runSlaw("watch", "list", "--json", "--workspace", root)
	require.NoError(t, err)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	byName := map[string]map[string]any{}
	for _, r := range rows {
		name, _ := r["name"].(string)
		byName[name] = r
	}
	assert.NotNil(t, byName["stuck"]["last_fired"])
	assert.Nil(t, byName["quiet"]["last_fired"])
	assert.NotNil(t, byName["quiet"]["last_checked"])
	assert.NotEmpty(t, byName["broken"]["error"])
	assert.Equal(t, "alert", byName["stuck"]["last_status"])

	out, _, err = runSlaw("watch", "show", "stuck", "--workspace", root)
	require.NoError(t, err)
	assert.Equal(t, wStuck, out, "the file as stored")
	out, _, err = runSlaw("watch", "show", "broken", "--workspace", root)
	require.Error(t, err)
	assert.Equal(t, 2, exitStatus(err))
	assert.Contains(t, out, "bogus: 1", "a broken file is still printed")
	assert.Contains(t, err.Error(), "does not pass validation")

	out, _, err = runSlaw("watch", "history", "--workspace", root)
	require.NoError(t, err)
	assert.Regexp(t, `alert\s+stuck\s+stuck_orders returned 2 rows`, out)
	assert.Regexp(t, `ok\s+quiet\s+order_count is 4; alerts above 10`, out)

	out, _, err = runSlaw("watch", "history", "stuck", "--json", "--workspace", root)
	require.NoError(t, err)
	var hist []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &hist))
	require.Len(t, hist, 1)
	assert.Equal(t, "alert", hist[0]["status"])
	assert.EqualValues(t, 2, hist[0]["rows"])

	// Newest first, and -n limits.
	for i := 0; i < 3; i++ {
		_, _, _ = runSlaw("watch", "check", "quiet", "--workspace", root)
	}
	out, _, err = runSlaw("watch", "history", "quiet", "-n", "2", "--json", "--workspace", root)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &hist))
	assert.Len(t, hist, 2)

	out, _, err = runSlaw("watch", "history", "nothing_yet", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "No checks recorded")
}

func TestWatch_ControlCharactersNeverReachTheTerminal(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/evil.yaml", "# \x1b]0;pwned\x07\nquestion: {query: order_count}\nwhen: {below: 10}\n")
	writeIn(t, root, "watches/x\x1b[31m.yaml", "x")
	writeIn(t, root, "watches/rows.yaml", "question: {query: customer_names}\nwhen: {rows: any}\n")
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{ID: "hostile_cells", Title: "Cells", SQL: "SELECT chr(27) || '[31mred' AS who"}))
	require.NoError(t, ws.Close())
	writeIn(t, root, "watches/cells.yaml", "question: {query: hostile_cells}\nwhen: {rows: any}\n")

	for _, args := range [][]string{{"list"}, {"show", "evil"}, {"check"}, {"history"}} {
		out, stderr, _ := runSlaw(append(append([]string{"watch"}, args...), "--workspace", root)...)
		assert.NotContains(t, out+stderr, "\x1b", args)
		assert.NotContains(t, out+stderr, "\x07", args)
	}
}

// What a check records stays in .slaw-local/ and never reaches the folder's
// tracked files or a bundle.
func TestWatchHistoryNeverLeavesLocalState(t *testing.T) {
	root := watchWorkspaceCLI(t)
	writeIn(t, root, "watches/stuck.yaml", wStuck)
	writeIn(t, root, "watches/quiet.yaml", "question: {query: order_count}\nwhen: {above: 10}\n")
	before := treeFiles(t, root)

	_, _, err := runSlaw("watch", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))
	_, _, err = runSlaw("watch", "check", "--workspace", root)
	assert.Equal(t, 1, exitStatus(err))

	after := treeFiles(t, root)
	var added []string
	for p := range after {
		if _, had := before[p]; !had {
			added = append(added, p)
		}
	}
	sort.Strings(added)
	for _, p := range added {
		assert.True(t, strings.HasPrefix(p, ".slaw-local/"), "a check wrote %s outside .slaw-local/", p)
	}
	assert.Contains(t, added, ".slaw-local/watches/history.jsonl")
	for p, sum := range before {
		assert.Equal(t, sum, after[p], "%s changed during a check", p)
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(ignore), ".slaw-local/", "the history is git-ignored with the rest of the local state")

	hist, err := os.ReadFile(filepath.Join(root, ".slaw-local", "watches", "history.jsonl"))
	require.NoError(t, err)
	assert.NotContains(t, string(hist), "acme", "the history holds no result rows")
	assert.NotContains(t, string(hist), "globex")
	assert.Equal(t, 4, strings.Count(string(hist), "\n"), "two checks of two watches")

	// An export of everything carries the watches and not a byte of history.
	bundlePath := filepath.Join(t.TempDir(), "all.slaw")
	_, stderr, err := runSlaw("export", "bundle", "-o", bundlePath, "--workspace", root)
	require.NoError(t, err, stderr)
	names, contents := zipEntries(t, bundlePath)
	assert.Contains(t, names, "watches/stuck.yaml")
	assert.Contains(t, names, "watches/quiet.yaml")
	for _, n := range names {
		assert.NotContains(t, n, ".slaw-local")
		assert.NotContains(t, n, "history")
	}
	for n, body := range contents {
		assert.NotContains(t, body, "stuck_orders returned 2 rows", n+" holds a history reason")
		assert.NotContains(t, body, `"status":"alert"`, n)
	}
}

func treeFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, ".duckdb") || strings.HasSuffix(rel, ".duckdb.wal") || strings.HasPrefix(rel, ".slaw-local/") {
			out[rel] = "" // the database is not a text file a check edits; local state may change
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	}))
	return out
}

func zipEntries(t *testing.T, path string) (names []string, contents map[string]string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	contents = map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		var buf bytes.Buffer
		_, err = buf.ReadFrom(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		names = append(names, f.Name)
		contents[f.Name] = buf.String()
	}
	return names, contents
}

func TestWatchBundle_RoundTripKeepsTheFileAndFollowsARenamedQuery(t *testing.T) {
	src := watchWorkspaceCLI(t)
	handWritten := "# why we care\n" + wStuck
	writeIn(t, src, "watches/stuck.yaml", handWritten)
	writeIn(t, src, "watches/revenue_high.yaml", "question: {metric: revenue}\nwhen: {above: 30}\n")
	out := filepath.Join(t.TempDir(), "share.slaw")
	stdout, stderr, err := runSlaw("export", "bundle", "-o", out, "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "2 watches")

	// Only the watches.
	only := filepath.Join(t.TempDir(), "only.slaw")
	stdout, stderr, err = runSlaw("export", "bundle", "-o", only, "--watches", "stuck", "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "1 watches")
	names, _ := zipEntries(t, only)
	assert.Equal(t, []string{"manifest.json", "watches/stuck.yaml"}, names)

	// Into a fresh workspace: the file arrives byte for byte, and checks the same.
	dst := newWS(t)
	stdout, stderr, err = runSlaw("import", out, "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "watch/stuck")
	got, err := os.ReadFile(filepath.Join(dst, "watches", "stuck.yaml"))
	require.NoError(t, err)
	assert.Equal(t, handWritten, string(got))
	list, _, err := runSlaw("watch", "list", "--workspace", dst)
	require.NoError(t, err)
	assert.Regexp(t, `stuck\s+query stuck_orders\s+any rows\s+never\s+never`, list, "history did not come along")

	// Into a workspace that has its own "stuck_orders" query and "stuck" watch:
	// the clash is renamed and the imported watch follows the renamed query.
	dst2 := newWS(t)
	ws, err := workspace.Open(dst2, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{ID: "stuck_orders", Title: "Mine", SQL: "SELECT 1"}))
	require.NoError(t, ws.Close())
	writeIn(t, dst2, "watches/stuck.yaml", "question: {query: stuck_orders}\nwhen: {rows: none}\n")
	stdout, stderr, err = runSlaw("import", out, "--workspace", dst2)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "stuck_2")
	renamed, err := os.ReadFile(filepath.Join(dst2, "watches", "stuck_2.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(renamed), "query: stuck_orders-2", "the watch asks the query that came with it")
	mine, err := os.ReadFile(filepath.Join(dst2, "watches", "stuck.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(mine), "rows: none", "the workspace's own watch is untouched")
}

func TestWatchBundle_AHostileWatchRefusesTheWholeBundleBeforeAnyWrite(t *testing.T) {
	src := watchWorkspaceCLI(t)
	writeIn(t, src, "queries/extra.sql", "-- slaw: {\"title\":\"Extra\"}\nSELECT 1\n")
	writeIn(t, src, "watches/fine.yaml", "question: {query: extra}\nwhen: {rows: any}\n")
	good := filepath.Join(t.TempDir(), "good.slaw")
	_, stderr, err := runSlaw("export", "bundle", "-o", good, "--workspace", src)
	require.NoError(t, err, stderr)

	// Rebuild the archive with the watch replaced by a document the strict
	// decoder refuses (an unknown key), keeping the manifest's checksums
	// honest so the refusal is the watch's, not a checksum's.
	for name, bad := range map[string]string{
		"unknown key":   "question: {query: extra}\nwhen: {rows: any}\nrun_as: root\n",
		"two kinds":     "question: {query: extra, metric: revenue}\nwhen: {rows: any}\n",
		"alias":         "base: &b {rows: any}\nquestion: {query: extra}\nwhen: *b\n",
		"bad quoting":   "question: {query: extra}\nwhen: {above: \"5\"}\n",
		"reversed":      "question: {query: extra}\nwhen: {outside: [9, 1]}\n",
		"not a mapping": "- a\n",
	} {
		t.Run(name, func(t *testing.T) {
			tampered := filepath.Join(t.TempDir(), "bad.slaw")
			rewriteBundleEntry(t, good, tampered, "watches/fine.yaml", bad)
			dst := newWS(t)
			_, _, err := runSlaw("import", tampered, "--workspace", dst)
			require.Error(t, err)
			assert.NoFileExists(t, filepath.Join(dst, "watches", "fine.yaml"))
			assert.NoFileExists(t, filepath.Join(dst, "queries", "extra.sql"), "nothing was written, not even the valid items")
		})
	}

	// A history path is not an entry a bundle may carry.
	smuggled := filepath.Join(t.TempDir(), "smuggled.slaw")
	addBundleEntry(t, good, smuggled, ".slaw-local/watches/history.jsonl", "{}\n")
	dst := newWS(t)
	_, _, err = runSlaw("import", smuggled, "--workspace", dst)
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(dst, ".slaw-local", "watches", "history.jsonl"))
}

// rewriteBundleEntry copies a bundle, replacing one entry's content and fixing
// the manifest's checksum and size so the archive still verifies.
func rewriteBundleEntry(t *testing.T, from, to, entry, body string) {
	t.Helper()
	names, contents := zipEntries(t, from)
	contents[entry] = body
	writeBundleZip(t, to, names, contents)
}

// addBundleEntry copies a bundle and adds an entry the manifest lists.
func addBundleEntry(t *testing.T, from, to, entry, body string) {
	t.Helper()
	names, contents := zipEntries(t, from)
	names = append(names, entry)
	contents[entry] = body
	var m bundle.Manifest
	require.NoError(t, json.Unmarshal([]byte(contents["manifest.json"]), &m))
	m.Contents = append(m.Contents, bundle.Entry{Path: entry, Kind: bundle.KindWatch, ID: "history"})
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	contents["manifest.json"] = string(raw)
	writeBundleZip(t, to, names, contents)
}

func writeBundleZip(t *testing.T, to string, names []string, contents map[string]string) {
	t.Helper()
	var m bundle.Manifest
	require.NoError(t, json.Unmarshal([]byte(contents["manifest.json"]), &m))
	for i, e := range m.Contents {
		body, ok := contents[e.Path]
		if !ok {
			continue
		}
		sum := sha256.Sum256([]byte(body))
		m.Contents[i].SHA256, m.Contents[i].Size = hex.EncodeToString(sum[:]), int64(len(body))
	}
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	contents["manifest.json"] = string(raw)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		require.NoError(t, err)
		_, err = w.Write([]byte(contents[n]))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	require.NoError(t, os.WriteFile(to, buf.Bytes(), 0o644))
}
