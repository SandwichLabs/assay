package prefs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func corruptFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "settings.json.corrupt-*"))
	require.NoError(t, err)
	return m
}

func TestModify_CorruptFileIsMovedAsideNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	bad := []byte(`{"theme": "nord", "uiScale": `) // a half-written file
	require.NoError(t, os.WriteFile(s.SettingsPath(), bad, 0o644))

	// A read still returns defaults and an error, and writes nothing.
	p, err := s.Load()
	require.ErrorIs(t, err, ErrCorrupt)
	assert.Equal(t, Defaults(), p)
	assert.Empty(t, corruptFiles(t, dir))

	require.NoError(t, s.Modify(func(p *Preferences) error {
		assert.Equal(t, Defaults(), *p, "modify starts from the defaults")
		p.ReduceMotion = true
		return nil
	}))

	moved := corruptFiles(t, dir)
	require.Len(t, moved, 1)
	got, err := os.ReadFile(moved[0])
	require.NoError(t, err)
	assert.Equal(t, bad, got, "the person's bytes are preserved")
	if info, err := os.Stat(moved[0]); err == nil {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	cur, err := s.Load()
	require.NoError(t, err)
	assert.True(t, cur.ReduceMotion)

	notice := s.TakeNotice()
	assert.Contains(t, notice, moved[0], "the notice names the moved file")
	assert.Equal(t, "", s.TakeNotice(), "said once")

	// A second write finds a good file: nothing more is moved.
	require.NoError(t, s.Modify(func(p *Preferences) error { p.UIScale = 110; return nil }))
	assert.Len(t, corruptFiles(t, dir), 1)
	assert.Equal(t, "", s.TakeNotice())
}

func TestModify_SaveAndRecentsDoNotOverwriteCorruptFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	require.NoError(t, os.WriteFile(s.SettingsPath(), []byte("not json"), 0o600))
	NewRecents(s, nil).Record(t.TempDir(), "id1", "One")
	require.Len(t, corruptFiles(t, dir), 1)
	assert.NotEmpty(t, s.TakeNotice())
}

func TestModify_FnErrorWritesNothing(t *testing.T) {
	s := NewStore(t.TempDir())
	require.NoError(t, s.Save(Preferences{Theme: "nord"}))
	err := s.Modify(func(p *Preferences) error { p.Theme = "x"; return fmt.Errorf("nope") })
	require.Error(t, err)
	p, _ := s.Load()
	assert.Equal(t, "nord", p.Theme)
}

func TestModify_UnreadableFileRefusesTheWrite(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// A directory where the file should be: a read error that is not a parse error.
	require.NoError(t, os.Mkdir(s.SettingsPath(), 0o700))
	err := s.Modify(func(p *Preferences) error { return nil })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing")
}

func TestModify_ConcurrentStoresAllLand(t *testing.T) {
	dir := t.TempDir()
	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := NewStore(dir) // a separate Store per goroutine: only the file lock orders them
			errs[i] = s.Modify(func(p *Preferences) error { return setKey(p, i) })
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err, "writer %d", i)
	}
	assertKeys(t, dir, n)
}

func setKey(p *Preferences, i int) error {
	if p.Extra == nil {
		p.Extra = map[string]json.RawMessage{}
	}
	p.Extra[fmt.Sprintf("k%d", i)] = json.RawMessage(`true`)
	return nil
}

func assertKeys(t *testing.T, dir string, n int) {
	t.Helper()
	p, err := NewStore(dir).Load()
	require.NoError(t, err)
	for i := 0; i < n; i++ {
		assert.Contains(t, p.Extra, fmt.Sprintf("k%d", i), "write %d was lost", i)
	}
}

// TestModify_TwoProcesses re-executes the test binary so the writers are real
// separate processes.
func TestModify_TwoProcesses(t *testing.T) {
	dir := t.TempDir()
	const perProc, procs = 10, 3
	var cmds []*exec.Cmd
	var outs []*strings.Builder
	for p := 0; p < procs; p++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperModifyProcess$")
		cmd.Env = append(os.Environ(), "SLAW_PREFS_HELPER_DIR="+dir, fmt.Sprintf("SLAW_PREFS_HELPER_BASE=%d", p*perProc))
		out := &strings.Builder{}
		cmd.Stdout, cmd.Stderr = out, out
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd)
		outs = append(outs, out)
	}
	for i, c := range cmds {
		require.NoError(t, c.Wait(), "helper %d output:\n%s", i, outs[i].String())
	}
	assertKeys(t, dir, perProc*procs)
}

func TestHelperModifyProcess(t *testing.T) {
	dir := os.Getenv("SLAW_PREFS_HELPER_DIR")
	if dir == "" {
		t.Skip("helper process for TestModify_TwoProcesses")
	}
	var base int
	_, err := fmt.Sscan(os.Getenv("SLAW_PREFS_HELPER_BASE"), &base)
	require.NoError(t, err)
	s := NewStore(dir)
	for i := 0; i < 10; i++ {
		require.NoError(t, s.Modify(func(p *Preferences) error { return setKey(p, base+i) }))
	}
}

func TestModify_LockTimeoutWhenAnotherProcessHoldsIt(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	s.lockTimeout = 50 * time.Millisecond

	release, err := s.lock() // this "process" holds the lock
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	other := NewStore(dir)
	other.lockTimeout = 50 * time.Millisecond
	start := time.Now()
	err = other.Modify(func(p *Preferences) error { p.Theme = "x"; return nil })
	if !assertLockSupported(t, err) {
		return
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another Slaw process")
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)
	_, statErr := os.Stat(other.SettingsPath())
	assert.True(t, os.IsNotExist(statErr), "a refused write leaves no file")
}

// assertLockSupported skips on platforms with no cross-process lock (the lock
// is a no-op there, so Modify succeeds).
func assertLockSupported(t *testing.T, err error) bool {
	t.Helper()
	if err == nil {
		t.Skip("no cross-process lock on this platform")
		return false
	}
	return true
}

func TestModify_LockFileIsPersistentAndPrivate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	require.NoError(t, s.Modify(func(p *Preferences) error { return nil }))
	info, err := os.Stat(filepath.Join(dir, lockName))
	require.NoError(t, err, "the lock file is never unlinked, so waiters share one inode")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestModify_SymlinkedLockFileIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "victim")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
	if err := os.Symlink(target, filepath.Join(dir, lockName)); err != nil {
		t.Skip("no symlinks")
	}
	err := NewStore(dir).Modify(func(p *Preferences) error { return nil })
	if err == nil {
		t.Skip("no cross-process lock on this platform")
	}
	got, _ := os.ReadFile(target)
	assert.Equal(t, "keep", string(got))
}

func TestRecents_StaleWindowDoesNotRollBackAnotherWindow(t *testing.T) {
	dir := t.TempDir()
	a := NewRecents(NewStore(dir), nil) // window A
	b := NewRecents(NewStore(dir), nil) // window B (another process)
	pa, err := a.Preferences()
	require.NoError(t, err)
	pb, err := b.Preferences()
	require.NoError(t, err)

	pa.Theme = "nord"
	require.NoError(t, a.SavePreferences(pa))
	pb.UIScale = 120 // B still holds the old theme
	require.NoError(t, b.SavePreferences(pb))

	got, err := NewStore(dir).Load()
	require.NoError(t, err)
	assert.Equal(t, "nord", got.Theme, "B's stale theme must not roll A's change back")
	assert.Equal(t, 120, got.UIScale)
}

func TestRecents_BadSubKeyIsWarned(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"updates": "oops", "mcpWorkspaces": 3}`), 0o600))
	var buf strings.Builder
	r := NewRecents(NewStore(dir), slog.New(slog.NewTextHandler(&buf, nil)))
	r.Record(t.TempDir(), "id", "n")
	assert.Contains(t, buf.String(), "key=updates")
	assert.Contains(t, buf.String(), "key=mcpWorkspaces")
}

func TestStore_NoticesAreRaisedUpFrontAndOnMove(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{"), 0o600))
	var pushed []string
	s := NewStore(dir).WithNotify(func(m string) { pushed = append(pushed, m) })
	r := NewRecents(s, nil)

	_, err := r.Preferences()
	require.ErrorIs(t, err, ErrCorrupt)
	_, _ = r.Preferences() // a second read does not repeat it
	require.Len(t, pushed, 1)
	assert.Contains(t, pushed[0], "default settings are in use")
	assert.Contains(t, pushed[0], "settings.json.corrupt-")
	assert.Equal(t, pushed[0], s.TakeNotice())

	require.NoError(t, s.Modify(func(p *Preferences) error { return nil }))
	require.Len(t, pushed, 2, "the move is pushed too")
	moved := corruptFiles(t, dir)
	require.Len(t, moved, 1)
	assert.Contains(t, pushed[1], moved[0])
}
