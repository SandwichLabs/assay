package bundle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/watches"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// Hand-written on purpose: comments and flow style that a re-encode would lose.
const watchStuck = `# who to nag
description: Orders stuck in pending
question:
  query: stuck_orders
  params: {days: 1}
when:
  rows: any
`

const watchRevenue = "question: {metric: net_revenue}\nwhen: {outside: [10, 90]}\n"

var watchFiles = map[string]string{
	"watches/stuck.yaml":       watchStuck,
	"watches/revenue_cap.yaml": watchRevenue,
}

func writeWatches(t *testing.T, ws workspace.Workspace, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(ws.Root(), filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
}

func TestWatches_RoundTripKeepsBytes(t *testing.T) {
	src, _ := newWS(t)
	writeWatches(t, src, watchFiles)
	ctx := context.Background()

	res, err := Export(ctx, src, ExportOptions{Selection: all(), AppVersion: "test"})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 2)
	for _, e := range res.Manifest.Contents {
		assert.Equal(t, KindWatch, e.Kind, e.Path)
	}
	assert.Equal(t, 2, res.Manifest.Counts().Watches)

	dst, _ := newWS(t)
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	pv := pkg.Preview(ctx, dst)
	require.Len(t, pv.Items, 2)
	titles := map[string]string{}
	for _, it := range pv.Items {
		assert.False(t, it.Conflict)
		titles[it.Key] = it.Title
	}
	assert.Equal(t, "Orders stuck in pending", titles["watch/stuck"])
	assert.Equal(t, "revenue_cap", titles["watch/revenue_cap"])

	out, err := pkg.Import(ctx, dst, ImportOptions{})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	for rel, want := range watchFiles {
		got, err := os.ReadFile(filepath.Join(dst.Root(), filepath.FromSlash(rel)))
		require.NoError(t, err, rel)
		assert.Equal(t, want, string(got), rel)
	}
	w, err := dst.Watches().Get(ctx, "stuck")
	require.NoError(t, err)
	assert.Equal(t, "stuck_orders", w.Question.Query)
}

func TestWatches_SelectAndContentsOf(t *testing.T) {
	src, _ := newWS(t)
	writeWatches(t, src, watchFiles)
	writeWatches(t, src, map[string]string{"watches/broken.yaml": "question: {query: q}\n"})
	ctx := context.Background()

	c, err := ContentsOf(ctx, src)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, it := range c.Watches {
		ids[it.ID] = it.Title
	}
	assert.Equal(t, map[string]string{"stuck": "Orders stuck in pending", "revenue_cap": "revenue_cap"}, ids, "an unreadable file is not offered")

	res, err := Export(ctx, src, ExportOptions{Selection: Selection{Watches: []string{"stuck"}}})
	require.NoError(t, err)
	require.Len(t, res.Manifest.Contents, 1)
	assert.Equal(t, "watches/stuck.yaml", res.Manifest.Contents[0].Path)

	res, err = Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	assert.Len(t, res.Manifest.Contents, 2)
	assert.Contains(t, strings.Join(res.Manifest.Warnings, "\n"), "watches/broken.yaml was skipped")

	_, err = Export(ctx, src, ExportOptions{Selection: Selection{Watches: []string{"broken"}}})
	require.Error(t, err, "asking for a broken file by name fails the export")
	_, err = Export(ctx, src, ExportOptions{Selection: Selection{Watches: []string{"Bad Name"}}})
	require.Error(t, err)
}

func TestWatches_OpenRefusesTheWholeBundleBeforeAnyWrite(t *testing.T) {
	good := func() map[string][]byte {
		return map[string][]byte{"watches/stuck.yaml": []byte(watchStuck), "watches/revenue_cap.yaml": []byte(watchRevenue)}
	}
	set := func(rel, body string) func(map[string][]byte) {
		return func(e map[string][]byte) { e[rel] = []byte(body) }
	}
	cases := map[string]func(map[string][]byte){
		"unknown key":      set("watches/stuck.yaml", watchStuck+"schedule: hourly\n"),
		"both questions":   set("watches/stuck.yaml", "question: {query: q, metric: m}\nwhen: {rows: any}\n"),
		"two conditions":   set("watches/stuck.yaml", "question: {query: q}\nwhen: {rows: any, above: 1}\n"),
		"quoted threshold": set("watches/stuck.yaml", "question: {query: q}\nwhen: {above: \"5\"}\n"),
		"reversed range":   set("watches/stuck.yaml", "question: {query: q}\nwhen: {outside: [9, 1]}\n"),
		"anchor":           set("watches/stuck.yaml", "q: &q {rows: any}\nquestion: {query: q}\nwhen: *q\n"),
		"oversize":         set("watches/stuck.yaml", watchStuck+"# "+strings.Repeat("x", watches.MaxYAMLBytes)+"\n"),
		"control char":     set("watches/stuck.yaml", watchStuck+"# \x1b[2J\n"),
		"upper-case name":  set("watches/Stuck.yaml", watchStuck),
		"yml extension":    set("watches/stuck.yml", watchStuck),
		"nested":           set("watches/sub/x.yaml", watchStuck),
		"history":          set(".slaw-local/watches/history.jsonl", "{}\n"),
		"history in dir":   set("watches/history.jsonl", "{}\n"),
		"lock file":        set(".slaw-local/watches/history.lock", ""),
		"traversal":        set("watches/../x.yaml", watchStuck),
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			entries := good()
			mutate(entries)
			_, err := Open(build(t, entries, nil), "")
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "damaged") || strings.Contains(err.Error(), "too large"), err.Error())
		})
	}
	t.Run("the good bundle opens", func(t *testing.T) {
		_, err := Open(build(t, good(), nil), "")
		require.NoError(t, err)
	})
}

func TestWatches_HistoryIsNeverABundleEntry(t *testing.T) {
	src, _ := newWS(t)
	writeWatches(t, src, watchFiles)
	ctx := context.Background()
	require.NoError(t, src.WatchHistory().Append(ctx, watches.Entry{Watch: "stuck", At: time.Now(), Status: watches.StatusAlert, Reason: "stuck_orders returned 2 rows"}))
	writeWatches(t, src, map[string]string{"watches/history.jsonl": "{}\n", ".slaw-local/watches/extra.yaml": watchStuck})

	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)
	for _, e := range res.Manifest.Contents {
		assert.Contains(t, []string{"watches/stuck.yaml", "watches/revenue_cap.yaml"}, e.Path)
	}
	assert.NotContains(t, string(res.Data), "returned 2 rows")
}

func TestWatches_ClashRenameFollowsTheQueryAndMetricThatCameWithIt(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	require.NoError(t, src.Queries().Create(ctx, &queries.Query{ID: "stuck_orders", Title: "Stuck", SQL: "SELECT 1"}))
	require.NoError(t, src.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Base", SQL: "SELECT 1 AS amount"}))
	writeSem(t, src, map[string]string{"semantics/metrics/net_revenue.yaml": strings.Replace(semMetric, "dimensions: [region]\n", "", 1)})
	writeWatches(t, src, watchFiles)
	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)

	dst, _ := newWS(t)
	require.NoError(t, dst.Queries().Create(ctx, &queries.Query{ID: "stuck_orders", Title: "Mine", SQL: "SELECT 2"}))
	writeSem(t, dst, map[string]string{"semantics/metrics/net_revenue.yaml": semMetric})
	writeWatches(t, dst, map[string]string{"watches/stuck.yaml": "question: {query: stuck_orders}\nwhen: {rows: none}\n"})

	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	out, err := pkg.Import(ctx, dst, ImportOptions{})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)

	byKey := map[string]Outcome{}
	for _, o := range out.Outcomes {
		byKey[o.Key] = o
	}
	assert.Equal(t, "renamed", byKey["watch/stuck"].Result)
	assert.Equal(t, "stuck_2", byKey["watch/stuck"].FinalID)
	renamed, err := os.ReadFile(filepath.Join(dst.Root(), "watches", "stuck_2.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(renamed), "query: stuck_orders-2", "the watch follows the renamed query")
	revenue, err := os.ReadFile(filepath.Join(dst.Root(), "watches", "revenue_cap.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(revenue), "metric: net_revenue_2", "and the renamed metric")
	assert.Contains(t, strings.Join(out.Warnings, "\n"), "watch stuck now asks the saved query")
	own, err := os.ReadFile(filepath.Join(dst.Root(), "watches", "stuck.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(own), "rows: none", "the workspace's own watch is untouched")
}

func TestWatches_OverwriteIsAgainstTheRevisionJustRead(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	writeWatches(t, src, map[string]string{"watches/stuck.yaml": watchStuck})
	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)

	dst, _ := newWS(t)
	writeWatches(t, dst, map[string]string{"watches/stuck.yaml": "question: {query: other}\nwhen: {rows: none}\n"})
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	pv := pkg.Preview(ctx, dst)
	require.Len(t, pv.Items, 1)
	assert.True(t, pv.Items[0].Conflict)

	// Someone edits the file after the preview: the overwrite is refused.
	writeWatches(t, dst, map[string]string{"watches/stuck.yaml": "question: {query: edited}\nwhen: {rows: none}\n"})
	out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionOverwrite})
	require.NoError(t, err)
	require.Equal(t, 1, out.Failed(), "%+v", out.Outcomes)
	got, err := os.ReadFile(filepath.Join(dst.Root(), "watches", "stuck.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "query: edited", "the newer edit was not clobbered")
}

func TestWatches_ASkippedMetricIsSaidToBeTheWorkspacesOwn(t *testing.T) {
	ctx := context.Background()
	src, _ := newWS(t)
	writeSem(t, src, map[string]string{"semantics/metrics/net_revenue.yaml": semMetric})
	writeWatches(t, src, map[string]string{"watches/revenue_cap.yaml": watchRevenue})
	res, err := Export(ctx, src, ExportOptions{Selection: all()})
	require.NoError(t, err)

	dst, _ := newWS(t)
	writeSem(t, dst, map[string]string{"semantics/metrics/net_revenue.yaml": semMetric + "# mine\n"})
	pkg, err := Open(res.Data, "")
	require.NoError(t, err)
	out, err := pkg.Import(ctx, dst, ImportOptions{OnConflict: ActionSkip})
	require.NoError(t, err)
	require.Zero(t, out.Failed(), "%+v", out.Outcomes)
	assert.Contains(t, strings.Join(out.Warnings, "\n"), `watch revenue_cap asks the metric "net_revenue", which was skipped`)
	got, err := os.ReadFile(filepath.Join(dst.Root(), "watches", "revenue_cap.yaml"))
	require.NoError(t, err)
	assert.Equal(t, watchRevenue, string(got), "the watch itself is imported as it was")
}
