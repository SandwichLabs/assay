package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/bundle"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/watches"
)

func addQuery(t *testing.T, app *App, id, sql string, params ...queries.Param) {
	t.Helper()
	require.NoError(t, app.ws.Queries().Create(context.Background(), &queries.Query{ID: id, Title: id, SQL: sql, Parameters: params}))
}

// appWithWatches has a local DuckDB with an orders table, a metric
// (net_revenue = 36 at its total) and the saved queries the watches ask.
func appWithWatches(t *testing.T) *App {
	t.Helper()
	app := appWithMetric(t)
	_, err := app.sess.DB().Exec(`CREATE TABLE orders (id INTEGER, customer VARCHAR, status VARCHAR);
		INSERT INTO orders VALUES (1, 'acme', 'pending'), (2, 'globex', 'pending'), (3, 'initech', 'done')`)
	require.NoError(t, err)
	addQuery(t, app, "pending_orders", "SELECT id, customer FROM orders WHERE status = $status",
		queries.Param{Name: "status", Type: "text", Default: "pending"})
	addQuery(t, app, "order_count", "SELECT count(*) AS n FROM orders")
	addQuery(t, app, "nothing", "SELECT id FROM orders WHERE id < 0")
	addQuery(t, app, "exfil", "COPY (SELECT 1) TO 'exfil.csv'")
	return app
}

func draftQuery(name, query string, when WatchWhen) WatchDraft {
	return WatchDraft{Name: name, Question: watches.Question{Query: query}, When: when}
}

func TestSaveWatch_RoundTripConflictAndDelete(t *testing.T) {
	app := appWithWatches(t)

	preview, err := app.PreviewWatch(WatchDraft{Name: "stuck", Description: "pending", Question: watches.Question{Query: "pending_orders", Params: map[string]string{"status": "pending", "blank": " "}}, When: WatchWhen{Rows: "any"}})
	require.NoError(t, err)
	assert.Contains(t, preview, "query: pending_orders")
	assert.Contains(t, preview, "status: pending")
	assert.NotContains(t, preview, "blank", "a blank param input means the default")
	_, err = os.Stat(filepath.Join(app.ws.Root(), "watches", "stuck.yaml"))
	assert.True(t, os.IsNotExist(err), "a preview writes nothing")

	rev, err := app.SaveWatch(WatchDraft{Name: "stuck", Question: watches.Question{Query: "pending_orders"}, When: WatchWhen{Rows: "any"}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	// Creating over an existing name is a conflict, not a replace.
	_, err = app.SaveWatch(WatchDraft{Name: "stuck", Question: watches.Question{Query: "order_count"}, When: WatchWhen{Rows: "none"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")

	// An edit against the revision it loaded works; a second against the old one conflicts.
	rev2, err := app.SaveWatch(WatchDraft{Name: "stuck", Description: "v2", Question: watches.Question{Query: "pending_orders"}, When: WatchWhen{Rows: "any"}, Rev: rev})
	require.NoError(t, err)
	assert.NotEqual(t, rev, rev2)
	_, err = app.SaveWatch(WatchDraft{Name: "stuck", Description: "mine", Question: watches.Question{Query: "pending_orders"}, When: WatchWhen{Rows: "any"}, Rev: rev})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
	// Keep mine.
	_, err = app.SaveWatch(WatchDraft{Name: "stuck", Description: "mine", Question: watches.Question{Query: "pending_orders"}, When: WatchWhen{Rows: "any"}, Rev: rev, Overwrite: true})
	require.NoError(t, err)

	d, err := app.GetWatch("stuck")
	require.NoError(t, err)
	assert.Equal(t, "mine", d.Description)
	assert.Contains(t, d.Text, "description: mine")
	assert.Equal(t, "any rows", d.WhenText)
	assert.Equal(t, "query pending_orders", d.QuestionText)

	err = app.DeleteWatch("stuck", rev2) // stale
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
	require.NoError(t, app.DeleteWatch("stuck", d.Rev))
	l, err := app.ListWatches()
	require.NoError(t, err)
	assert.Empty(t, l.Watches)
}

func TestSaveWatch_ValidationComesFromTheDecoder(t *testing.T) {
	app := appWithWatches(t)
	cases := map[string]WatchDraft{
		"not a plain decimal": draftQuery("w", "order_count", WatchWhen{Above: "1e3"}),
		"no condition":        draftQuery("w", "order_count", WatchWhen{}),
		"two conditions":      draftQuery("w", "order_count", WatchWhen{Rows: "any", Above: "1"}),
		"bad name":            draftQuery("Bad Name", "order_count", WatchWhen{Rows: "any"}),
		"no question":         {Name: "w", When: WatchWhen{Rows: "any"}},
		"low above high":      draftQuery("w", "order_count", WatchWhen{Outside: []string{"9", "1"}}),
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := app.SaveWatch(d)
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "invalid watch:", "the prefix every message would repeat is dropped")
			_, perr := app.PreviewWatch(d)
			require.Error(t, perr)
		})
	}
	l, err := app.ListWatches()
	require.NoError(t, err)
	assert.Empty(t, l.Watches, "nothing refused was written")
}

func TestCheckWatches_OutcomesHistoryAndBadge(t *testing.T) {
	app := appWithWatches(t)
	for _, d := range []WatchDraft{
		draftQuery("a_ok", "order_count", WatchWhen{Above: "100"}),
		draftQuery("b_alert", "order_count", WatchWhen{Below: "10"}),
		draftQuery("c_rows", "pending_orders", WatchWhen{Rows: "any"}),
		draftQuery("d_none", "nothing", WatchWhen{Rows: "none"}),
		draftQuery("e_missing", "no_such_query", WatchWhen{Rows: "any"}),
		draftQuery("f_refused", "exfil", WatchWhen{Rows: "any"}),
		{Name: "g_metric", Question: watches.Question{Metric: "net_revenue"}, When: WatchWhen{Outside: []string{"0", "30"}}},
	} {
		_, err := app.SaveWatch(d)
		require.NoError(t, err, d.Name)
	}
	// A hand-written broken file lists with its error and checks as an error.
	require.NoError(t, os.WriteFile(filepath.Join(app.ws.Root(), "watches", "h_broken.yaml"), []byte("question: {query: x\n"), 0o600))

	run, err := app.CheckWatches(nil)
	require.NoError(t, err)
	assert.False(t, run.Cancelled)
	by := map[string]string{}
	for _, r := range run.Results {
		by[r.Watch] = string(r.Status)
	}
	assert.Equal(t, map[string]string{
		"a_ok": "ok", "b_alert": "alert", "c_rows": "alert", "d_none": "alert",
		"e_missing": "error", "f_refused": "error", "g_metric": "alert", "h_broken": "error",
	}, by)
	for _, r := range run.Results {
		if r.Watch == "c_rows" {
			require.NotNil(t, r.Sample, "the sample rows travel with the result")
			assert.Equal(t, []string{"id", "customer"}, r.Sample.Columns)
			assert.Len(t, r.Sample.Rows, 2)
		}
		if r.Watch == "f_refused" {
			assert.Contains(t, r.Reason, "refused")
		}
	}

	l, err := app.ListWatches()
	require.NoError(t, err)
	require.Len(t, l.Watches, 8)
	assert.Equal(t, 4, l.Alerts, "b_alert, c_rows, d_none and g_metric fired")
	byName := map[string]WatchSummary{}
	for _, w := range l.Watches {
		byName[w.Name] = w
	}
	assert.Equal(t, "alert", byName["b_alert"].LastStatus)
	require.NotNil(t, byName["b_alert"].Last)
	assert.Contains(t, byName["b_alert"].Last.Reason, "below")
	assert.NotNil(t, byName["b_alert"].LastFired)
	assert.Nil(t, byName["a_ok"].LastFired)
	assert.NotEmpty(t, byName["h_broken"].Error)
	assert.Empty(t, byName["h_broken"].QuestionText)
	assert.Equal(t, "ok", byName["a_ok"].LastStatus)
	assert.Equal(t, "outside 0 to 30", byName["g_metric"].WhenText)
	assert.Equal(t, WatchWhen{Outside: []string{"0", "30"}}, byName["g_metric"].When)

	// A named check checks only that watch, and the history gains an entry.
	run, err = app.CheckWatches([]string{"a_ok"})
	require.NoError(t, err)
	require.Len(t, run.Results, 1)
	h, err := app.WatchHistory("a_ok", 0)
	require.NoError(t, err)
	assert.Len(t, h, 2)
	assert.False(t, h[0].At.Before(h[1].At), "newest first")
	h, err = app.WatchHistory("a_ok", 1)
	require.NoError(t, err)
	assert.Len(t, h, 1)

	_, err = app.CheckWatches([]string{"nope"})
	require.Error(t, err, "an unknown name is an error before anything runs")
}

func TestCheckWatches_StopKeepsWhatWasChecked(t *testing.T) {
	app := appWithWatches(t)
	addQuery(t, app, "slow", "SELECT sum(hash(a.range * b.range)) AS v FROM range(100000) a, range(100000) b")
	_, err := app.SaveWatch(draftQuery("a_fast", "order_count", WatchWhen{Above: "100"}))
	require.NoError(t, err)
	_, err = app.SaveWatch(draftQuery("b_slow", "slow", WatchWhen{Above: "0"}))
	require.NoError(t, err)

	type out struct {
		run *WatchCheckRun
		err error
	}
	done := make(chan out, 1)
	go func() {
		r, e := app.CheckWatches(nil)
		done <- out{r, e}
	}()
	require.Eventually(t, func() bool {
		h, _ := app.WatchHistory("a_fast", 0)
		return len(h) == 1 // the fast watch is recorded; the slow one is running
	}, 10*time.Second, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	app.CancelRun(runKeyWatches)

	select {
	case o := <-done:
		require.NoError(t, o.err)
		assert.True(t, o.run.Cancelled)
		require.Len(t, o.run.Results, 1, "the interrupted watch is left out")
		assert.Equal(t, "a_fast", o.run.Results[0].Watch)
	case <-time.After(20 * time.Second):
		t.Fatal("Stop did not end the check")
	}
	h, err := app.WatchHistory("b_slow", 0)
	require.NoError(t, err)
	assert.Empty(t, h, "a stopped check says nothing about the watch")
}

func TestExportBundle_OffersAndWritesWatches(t *testing.T) {
	app := appWithWatches(t)
	_, err := app.SaveWatch(draftQuery("stuck", "pending_orders", WatchWhen{Rows: "any"}))
	require.NoError(t, err)
	_, err = app.SaveWatch(draftQuery("volume", "order_count", WatchWhen{Below: "3"}))
	require.NoError(t, err)
	_, err = app.CheckWatches(nil)
	require.NoError(t, err)

	content, err := app.GetBundleContent()
	require.NoError(t, err)
	ids := []string{}
	for _, w := range content.Watches {
		ids = append(ids, w.ID)
	}
	assert.Equal(t, []string{"stuck", "volume"}, ids)

	path := filepath.Join(t.TempDir(), "w.slaw")
	res, err := app.ExportBundle(BundleExportRequest{Path: path, Selection: bundle.Selection{Watches: []string{"stuck"}}})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Watches)
}
