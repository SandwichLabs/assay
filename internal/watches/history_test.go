package watches

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/filelock"
)

var t0 = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

func entry(watch string, i int, status Status) Entry {
	return Entry{Watch: watch, Rev: "r", At: t0.Add(time.Duration(i) * time.Minute), Status: status, Reason: fmt.Sprintf("%s check %d", watch, i)}
}

func historyFilePath(root string) string { return filepath.Join(root, filepath.FromSlash(HistoryPath)) }

func TestFileHistory_AppendAndRead(t *testing.T) {
	root := t.TempDir()
	h := NewFileHistory(root)
	ctx := context.Background()

	got, err := h.Entries(ctx, "")
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got, "no history yet")
	_, statErr := os.Stat(filepath.Join(root, ".slaw-local"))
	assert.True(t, os.IsNotExist(statErr), "reading creates nothing")

	v, n := json.Number("1240"), 3
	require.NoError(t, h.Append(ctx, Entry{Watch: "a", Rev: "r1", At: t0, Status: StatusAlert, Value: v, Reason: "a is 1,240, above 1,000", DurationMS: 12}))
	require.NoError(t, h.Append(ctx, Entry{Watch: "b", At: t0.Add(time.Minute), Status: StatusOK, Rows: &n, RowsCapped: true, Reason: "b returned 1,000+ rows"}))
	require.NoError(t, h.Append(ctx, entry("a", 2, StatusError)))

	all, err := h.Entries(ctx, "")
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, []string{"a", "b", "a"}, []string{all[0].Watch, all[1].Watch, all[2].Watch}, "oldest first")
	assert.Equal(t, json.Number("1240"), all[0].Value)
	require.NotNil(t, all[1].Rows)
	assert.Equal(t, 3, *all[1].Rows)
	assert.True(t, all[1].RowsCapped)
	assert.True(t, all[0].At.Equal(t0))

	onlyA, err := h.Entries(ctx, "a")
	require.NoError(t, err)
	assert.Len(t, onlyA, 2)

	sum := Summarize(all)
	require.NotNil(t, sum["a"].LastFired)
	assert.True(t, sum["a"].LastFired.Equal(t0))
	assert.True(t, sum["a"].LastChecked.Equal(t0.Add(2*time.Minute)))
	assert.Equal(t, StatusError, sum["a"].LastStatus)
	assert.Equal(t, 2, sum["a"].Checks)
	assert.Nil(t, sum["b"].LastFired, "an ok check never fired")

	// The file and its folder are private to the person.
	fi, err := os.Stat(historyFilePath(root))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	di, err := os.Stat(filepath.Dir(historyFilePath(root)))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), di.Mode().Perm())
}

func TestFileHistory_KeepsTheNewestPerWatch(t *testing.T) {
	root := t.TempDir()
	h := NewFileHistory(root, WithLimits(5, DefaultMaxBytes))
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		require.NoError(t, h.Append(ctx, entry("busy", i, StatusOK)))
		if i < 2 {
			require.NoError(t, h.Append(ctx, entry("quiet", i, StatusOK)))
		}
	}
	busy, err := h.Entries(ctx, "busy")
	require.NoError(t, err)
	require.Len(t, busy, 5)
	assert.Equal(t, "busy check 7", busy[0].Reason, "the oldest were dropped")
	assert.Equal(t, "busy check 11", busy[4].Reason)
	quiet, err := h.Entries(ctx, "quiet")
	require.NoError(t, err)
	assert.Len(t, quiet, 2, "a quiet watch keeps its own entries however busy another is")
}

func TestFileHistory_DefaultKeepsTwoHundredPerWatch(t *testing.T) {
	assert.Equal(t, 200, DefaultPerWatch)
	assert.Equal(t, 5<<20, DefaultMaxBytes)
	root := t.TempDir()
	h := NewFileHistory(root)
	ctx := context.Background()
	for i := 0; i < 205; i++ {
		require.NoError(t, h.Append(ctx, entry("w", i, StatusOK)))
	}
	got, err := h.Entries(ctx, "w")
	require.NoError(t, err)
	require.Len(t, got, 200)
	assert.Equal(t, "w check 5", got[0].Reason)
}

func TestFileHistory_SizeCapDropsTheOldestFirst(t *testing.T) {
	root := t.TempDir()
	line := len(encodeLine(entry("w", 0, StatusOK)))
	h := NewFileHistory(root, WithLimits(1000, int64(line*10)))
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		require.NoError(t, h.Append(ctx, entry("w", i, StatusOK)))
	}
	got, err := h.Entries(ctx, "")
	require.NoError(t, err)
	assert.LessOrEqual(t, len(got), 10)
	assert.Equal(t, "w check 29", got[len(got)-1].Reason, "the newest is always kept")
	fi, err := os.Stat(historyFilePath(root))
	require.NoError(t, err)
	assert.LessOrEqual(t, fi.Size(), int64(line*10+line), "the file stays within the cap")
}

func TestPrune_IsStableAndKeepsOrder(t *testing.T) {
	var in []Entry
	for i := 0; i < 6; i++ {
		in = append(in, entry("a", i, StatusOK), entry("b", i, StatusOK))
	}
	out := Prune(in, 2, 0)
	require.Len(t, out, 4)
	assert.Equal(t, "a check 4", out[0].Reason)
	assert.Equal(t, "b check 4", out[1].Reason)
	assert.Equal(t, "a check 5", out[2].Reason)
	assert.Equal(t, "b check 5", out[3].Reason)
	assert.Len(t, Prune(in, 0, 0), 12, "zero means no per-watch cap")
}

func TestFileHistory_NeverWritesResultRows(t *testing.T) {
	// The Entry type has no field a result row could travel in, and a reason
	// is capped, so an error message that quotes a value stays one short line.
	root := t.TempDir()
	h := NewFileHistory(root)
	ctx := context.Background()
	long := "query execution failed: failed to execute query on duckdb: Conversion Error: could not convert string 'Alice Anderson' to INT32\n\nLINE 1: SELECT CAST(email AS INT)\n" + strings.Repeat("x", 2000)
	// What a check stores for a database error is its class, never its text.
	require.NoError(t, h.Append(ctx, Entry{Watch: "w", At: t0, Status: StatusError, Reason: "could not run: " + ErrorClass(long) + "\nsecond line\x1b[31m"}))
	raw, err := os.ReadFile(historyFilePath(root))
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(raw), "\n"), "one line per check")
	var e Entry
	require.NoError(t, json.Unmarshal(raw, &e))
	assert.NotContains(t, string(raw), "Alice")
	assert.NotContains(t, string(raw), "'")
	assert.Contains(t, e.Reason, "could not run: Conversion Error")
	assert.LessOrEqual(t, len([]rune(e.Reason)), MaxReasonRunes)
	assert.NotContains(t, e.Reason, "\x1b")
	assert.NotContains(t, e.Reason, "\n")

	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	for k := range fields {
		assert.Contains(t, []string{"watch", "rev", "at", "status", "value", "rows", "rows_capped", "reason", "duration_ms"}, k)
	}
}

func TestFileHistory_RefusesAnEntryItCannotKeep(t *testing.T) {
	h := NewFileHistory(t.TempDir())
	ctx := context.Background()
	assert.Error(t, h.Append(ctx, Entry{Watch: "Bad Name", At: t0, Status: StatusOK}))
	assert.Error(t, h.Append(ctx, Entry{Watch: "w", At: t0, Status: "weird"}))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	assert.ErrorIs(t, h.Append(canceled, entry("w", 0, StatusOK)), context.Canceled)
}

func TestFileHistory_SkipsAndRepairsGarbage(t *testing.T) {
	root := t.TempDir()
	h := NewFileHistory(root)
	ctx := context.Background()
	require.NoError(t, h.Append(ctx, entry("w", 0, StatusOK)))
	f, err := os.OpenFile(historyFilePath(root), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("not json\n{\"watch\":\"BAD NAME\",\"status\":\"ok\"}\n{\"watch\":\"w\",\"status\":\"nope\"}\n" + strings.Repeat("z", maxLineBytes+5) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got, err := h.Entries(ctx, "")
	require.NoError(t, err)
	assert.Len(t, got, 1, "bad lines are skipped on read")

	require.NoError(t, h.Append(ctx, entry("w", 1, StatusOK)))
	raw, err := os.ReadFile(historyFilePath(root))
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(raw), "\n"), "the next write drops what could not be read")
}

func TestFileHistory_RefusesLinks(t *testing.T) {
	ctx := context.Background()
	t.Run("history file", func(t *testing.T) {
		root := t.TempDir()
		h := NewFileHistory(root)
		require.NoError(t, h.Append(ctx, entry("w", 0, StatusOK)))
		target := filepath.Join(t.TempDir(), "elsewhere")
		require.NoError(t, os.WriteFile(target, []byte("keep me\n"), 0o600))
		require.NoError(t, os.Remove(historyFilePath(root)))
		if err := os.Symlink(target, historyFilePath(root)); err != nil {
			t.Skip("symlinks are not available here")
		}
		assert.Error(t, h.Append(ctx, entry("w", 1, StatusOK)))
		_, err := h.Entries(ctx, "")
		assert.Error(t, err)
		b, _ := os.ReadFile(target)
		assert.Equal(t, "keep me\n", string(b), "nothing was written through the link")
	})
	t.Run("folder", func(t *testing.T) {
		root := t.TempDir()
		elsewhere := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".slaw-local"), 0o755))
		if err := os.Symlink(elsewhere, filepath.Join(root, ".slaw-local", "watches")); err != nil {
			t.Skip("symlinks are not available here")
		}
		h := NewFileHistory(root)
		assert.Error(t, h.Append(ctx, entry("w", 0, StatusOK)))
		_, err := h.Entries(ctx, "")
		assert.Error(t, err)
		ents, _ := os.ReadDir(elsewhere)
		assert.Empty(t, ents, "nothing was written through the link")
	})
}

func TestFileHistory_ConcurrentWritersLoseNothing(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	const writers, each = 6, 15
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each writer has its own FileHistory, as separate processes do:
			// only the file lock serializes them.
			h := NewFileHistory(root, WithLimits(10000, DefaultMaxBytes))
			for i := 0; i < each; i++ {
				assert.NoError(t, h.Append(ctx, entry(fmt.Sprintf("w%d", w), i, StatusOK)))
			}
		}()
	}
	wg.Wait()
	got, err := NewFileHistory(root).Entries(ctx, "")
	require.NoError(t, err)
	assert.Len(t, got, writers*each)
}

func TestFileHistory_AppendWaitsForTheFileLock(t *testing.T) {
	root := t.TempDir()
	h := NewFileHistory(root)
	ctx := context.Background()
	require.NoError(t, h.Append(ctx, entry("w", 0, StatusOK)))

	dir, err := os.Open(filepath.Dir(historyFilePath(root)))
	require.NoError(t, err)
	defer dir.Close() //nolint:errcheck
	held, err := filelock.AcquirePersistentAt(dir, lockName)
	if err != nil {
		t.Skipf("no file lock here: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- h.Append(ctx, entry("w", 1, StatusOK)) }()
	select {
	case err := <-done:
		_ = held.Release()
		t.Fatalf("Append finished while another holder had the lock (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, held.Release())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Append never finished after the lock was released")
	}
	got, err := h.Entries(ctx, "")
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestFileHistory_ValueIsExactAndBounded(t *testing.T) {
	root := t.TempDir()
	h := NewFileHistory(root)
	ctx := context.Background()
	const huge = "123456789012345678901234567890" // above 2^64
	e := entry("w", 0, StatusAlert)
	e.Value = json.Number(huge)
	require.NoError(t, h.Append(ctx, e))
	got, err := h.Entries(ctx, "w")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, json.Number(huge), got[0].Value, "no float rounding")
	raw, err := os.ReadFile(historyFilePath(root))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"value":`+huge)

	for _, bad := range []string{"1e", "abc", "1,5", "NaN", strings.Repeat("9", MaxValueLen+1)} {
		e.Value = json.Number(bad)
		assert.Error(t, h.Append(ctx, e), bad)
	}
	// A hand-edited line with a bad value is skipped on read.
	f, err := os.OpenFile(historyFilePath(root), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(`{"watch":"w","status":"ok","value":"x","reason":"r"}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	got, err = h.Entries(ctx, "w")
	require.NoError(t, err)
	assert.Len(t, got, 1)
}
