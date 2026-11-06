package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/watches"
)

const watchDoc = "question:\n  query: base\nwhen:\n  rows: any\n"

func TestClassify_Watches(t *testing.T) {
	k, id, ok := classify("watches/stuck_orders.yaml")
	assert.True(t, ok)
	assert.Equal(t, KindWatch, k)
	assert.Equal(t, "stuck_orders", id)
	for _, rel := range []string{
		"watches/.stuck_orders.yaml.swp",
		"watches/stuck_orders.yaml~",
		"watches/stuck_orders.txt",
		"watches/a/b.yaml",
		"watches/history.jsonl",
		".slaw-local/watches/history.jsonl",
		".slaw-local/watches/history.lock",
	} {
		_, _, ok := classify(rel)
		assert.False(t, ok, rel+" is not a watch")
	}
}

func TestWatcher_WatchesAreReportedAndOwnWritesAreNot(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	w := startWatcher(t, ws)

	wt, err := watches.Decode("stuck_orders", []byte(watchDoc))
	require.NoError(t, err)
	require.NoError(t, ws.Watches().Put(ctx, wt))
	wt.Description = "edited"
	require.NoError(t, ws.Watches().Put(ctx, wt))
	require.NoError(t, ws.Watches().Delete(ctx, "stuck_orders"))
	expectQuiet(t, w)

	// A git pull creates the folder and the file at once.
	dir := filepath.Join(root, "watches")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte("x"), 0o644))
	assert.Equal(t, Change{Kind: KindWatch, Op: OpCreated, ID: "other", Path: "watches/other.yaml"}, next(t, w))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.yaml"), []byte("y"), 0o644))
	assert.Equal(t, Change{Kind: KindWatch, Op: OpChanged, ID: "other", Path: "watches/other.yaml"}, next(t, w))
	require.NoError(t, os.Remove(filepath.Join(dir, "other.yaml")))
	assert.Equal(t, Change{Kind: KindWatch, Op: OpDeleted, ID: "other", Path: "watches/other.yaml"}, next(t, w))
}

func TestWatcher_TheHistoryIsNotAWatchChange(t *testing.T) {
	ws, _ := newWorkspace(t)
	w := startWatcher(t, ws)
	require.NoError(t, ws.WatchHistory().Append(context.Background(), watches.Entry{Watch: "w", Status: watches.StatusOK, Reason: "ok"}))
	expectQuiet(t, w)
}
