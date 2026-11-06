// Package prefs stores app-global user preferences for Slaw — the
// settings that belong to the person, not to any one workspace (theme,
// appearance). They live outside every workspace folder so that switching
// workspaces never changes how the app looks, and so a repository shared with
// someone else never carries the author's UI preferences along.
//
// Storage is a small JSON document at <config dir>/slaw/settings.json, where
// <config dir> is os.UserConfigDir() (~/.config on Linux, ~/Library/Application
// Support on macOS) unless SLAW_CONFIG_DIR overrides it. User-authored theme
// files live beside it in <config dir>/slaw/themes/*.json — the VS Code model:
// drop a file in the folder and it shows up in the picker.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SandwichLabs/slaw/internal/filelock"
)

// ThemeSystem is the Preferences.Theme value meaning "follow the OS light/dark
// setting, using SystemLight / SystemDark".
const ThemeSystem = "system"

// Default theme ids. They must match built-in theme ids registered in the
// Studio frontend (studio/frontend/src/theme/builtin.ts).
const (
	DefaultTheme      = "schoolhouse-light"
	DefaultThemeLight = "schoolhouse-light"
	DefaultThemeDark  = "schoolhouse-dark"
)

// Preferences is the persisted document. Unknown fields round-trip untouched
// so an older Studio never strips settings written by a newer one.
type Preferences struct {
	// Theme is a theme id, or ThemeSystem to follow the OS appearance.
	Theme string `json:"theme"`
	// SystemLight / SystemDark are the theme ids used when Theme == ThemeSystem.
	SystemLight string `json:"systemLight"`
	SystemDark  string `json:"systemDark"`
	// ReduceMotion disables the hover/press transitions and slide-ins.
	ReduceMotion bool `json:"reduceMotion"`
	// UIScale is a percentage (100 = default). Bounded by Normalize.
	UIScale int `json:"uiScale"`

	// Extra carries fields this build does not know about.
	Extra map[string]json.RawMessage `json:"-"`
}

// Defaults returns the preferences a fresh install starts with.
func Defaults() Preferences {
	return Preferences{
		Theme:       DefaultTheme,
		SystemLight: DefaultThemeLight,
		SystemDark:  DefaultThemeDark,
		UIScale:     100,
	}
}

// Normalize fills blanks with defaults and clamps out-of-range values so a
// hand-edited settings file can never leave the UI in an unusable state.
func (p Preferences) Normalize() Preferences {
	d := Defaults()
	if strings.TrimSpace(p.Theme) == "" {
		p.Theme = d.Theme
	}
	if strings.TrimSpace(p.SystemLight) == "" {
		p.SystemLight = d.SystemLight
	}
	if strings.TrimSpace(p.SystemDark) == "" {
		p.SystemDark = d.SystemDark
	}
	switch {
	case p.UIScale == 0:
		p.UIScale = d.UIScale
	case p.UIScale < 75:
		p.UIScale = 75
	case p.UIScale > 150:
		p.UIScale = 150
	}
	return p
}

// MarshalJSON writes known fields plus any Extra fields carried through.
func (p Preferences) MarshalJSON() ([]byte, error) {
	type known Preferences
	base, err := json.Marshal(known(p))
	if err != nil {
		return nil, err
	}
	if len(p.Extra) == 0 {
		return base, nil
	}
	merged := map[string]json.RawMessage{}
	for k, v := range p.Extra {
		merged[k] = v
	}
	var ours map[string]json.RawMessage
	if err := json.Unmarshal(base, &ours); err != nil {
		return nil, err
	}
	for k, v := range ours { // known fields always win
		merged[k] = v
	}
	return json.Marshal(merged)
}

// UnmarshalJSON reads known fields and stashes the rest in Extra.
func (p *Preferences) UnmarshalJSON(data []byte) error {
	type known Preferences
	var k known
	if err := json.Unmarshal(data, &k); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for _, name := range []string{"theme", "systemLight", "systemDark", "reduceMotion", "uiScale"} {
		delete(all, name)
	}
	*p = Preferences(k)
	if len(all) > 0 {
		p.Extra = all
	}
	return nil
}

// ThemeFile is one user-authored theme found in the themes directory. The
// JSON is returned verbatim; the frontend owns validation so that the rules
// live in exactly one place (studio/frontend/src/theme/schema.ts).
type ThemeFile struct {
	// Path is the absolute path of the file (shown in Settings so the user can
	// find what they are editing).
	Path string `json:"path"`
	// Filename is the base name, e.g. "nord.json".
	Filename string `json:"filename"`
	// JSON is the raw file contents.
	JSON string `json:"json"`
}

// MaxThemeFileBytes bounds a single theme file. Anything larger is skipped
// (and reported) rather than read into memory — a theme is a few KB.
const MaxThemeFileBytes = 256 * 1024

// Store reads and writes the preferences document and lists user themes.
//
// Every write goes through Modify: one in-process mutex plus a cross-process
// lock file in the config dir around read, change and atomic replace, so two
// Studio windows, or Studio and `slaw update`, never drop each other's change.
type Store struct {
	mu          sync.Mutex
	dir         string
	lockTimeout time.Duration // 0 = defaultLockTimeout
	notice      string        // set when a corrupt file was moved aside; see TakeNotice
	logger      *slog.Logger  // nil = silent
	notify      func(string)  // called (with s.mu held; must not call the Store) when a notice is raised
	warned      bool          // the "unreadable, using defaults" notice was raised
}

// ErrCorrupt is wrapped by the error Load returns when settings.json exists but
// is not valid preferences JSON.
var ErrCorrupt = errors.New("preferences file is corrupt")

// lockName is the cross-process lock file inside the config dir. It is empty
// and stays in place between writes; the kernel flock, not the file, is the lock.
const lockName = "settings.lock"

// defaultLockTimeout bounds how long Modify waits for another process's write.
// A write is a few KB, so waiting longer means something is wedged.
const defaultLockTimeout = 5 * time.Second

// DefaultDir returns the per-user config directory Slaw uses:
// $SLAW_CONFIG_DIR if set to an absolute path (a relative one is ignored with
// a warning to logger, which may be nil), else os.UserConfigDir()/slaw.
func DefaultDir(logger *slog.Logger) (string, error) {
	if d := strings.TrimSpace(os.Getenv("SLAW_CONFIG_DIR")); d != "" {
		if filepath.IsAbs(d) {
			return d, nil
		}
		// A relative directory would resolve against whatever the working
		// directory happens to be, so a different folder per launch (and
		// inside a cloned repository when run from one): ignore it.
		if logger != nil {
			logger.Warn("ignoring SLAW_CONFIG_DIR: it must be an absolute path", "value", d)
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to locate user config dir: %w", err)
	}
	return filepath.Join(base, "slaw"), nil
}

// NewStore returns a Store rooted at dir. The directory is created lazily on
// first Save / EnsureThemesDir, never on construction.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// WithLogger makes the store log a warning when it moves a corrupt settings
// file aside (so a CLI run with no UI still leaves a trace). It returns s.
func (s *Store) WithLogger(l *slog.Logger) *Store {
	s.logger = l
	return s
}

// WithNotify registers fn to be called whenever a notice is raised (a corrupt
// file found at startup, or moved aside by a write), so a UI can show it at
// once. fn runs with the Store's mutex held and must not call back into it.
func (s *Store) WithNotify(fn func(string)) *Store {
	s.notify = fn
	return s
}

// raise records a notice for TakeNotice and tells the notify hook. s.mu is held.
func (s *Store) raise(msg string) {
	s.notice = msg
	if s.logger != nil {
		s.logger.Warn(msg)
	}
	if s.notify != nil {
		s.notify(msg)
	}
}

// NoteUnreadable raises, once per Store, the up-front notice for a settings
// file that does not parse: defaults are in use, and the next change keeps a
// copy of the file. Callers pass the error Load returned; other errors are
// ignored.
func (s *Store) NoteUnreadable(err error) {
	if !errors.Is(err, ErrCorrupt) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.warned {
		return
	}
	s.warned = true
	s.raise(fmt.Sprintf("Your settings file %s could not be read, so default settings are in use. The next time Slaw saves a setting it keeps a copy of that file as settings.json.corrupt-<time> beside it and starts over from the defaults.", s.SettingsPath()))
}

// Dir returns the store's root directory.
func (s *Store) Dir() string { return s.dir }

// SettingsPath returns the path of the settings document.
func (s *Store) SettingsPath() string { return filepath.Join(s.dir, "settings.json") }

// ThemesDir returns the directory scanned for user theme files.
func (s *Store) ThemesDir() string { return filepath.Join(s.dir, "themes") }

// Load reads the preferences. A missing file yields Defaults(); a corrupt file
// yields Defaults() plus an error wrapping ErrCorrupt so the caller can surface
// it without the UI failing to start. Load never writes: a caller that wants to
// change the document uses Modify, which moves a corrupt file aside first
// instead of overwriting it.
func (s *Store) Load() (Preferences, error) {
	return s.read()
}

// read takes no lock: the document is replaced by rename, so a reader sees the
// old or the new file, never a torn one, and never waits on a writer.
func (s *Store) read() (Preferences, error) {
	data, err := os.ReadFile(s.SettingsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("failed to read preferences: %w", err)
	}
	var p Preferences
	if err := json.Unmarshal(data, &p); err != nil {
		return Defaults(), fmt.Errorf("failed to parse %s: %w: %w", s.SettingsPath(), ErrCorrupt, err)
	}
	return p.Normalize(), nil
}

// Save replaces the whole document with p (under the same locks as Modify).
// Callers that change one part of the document should use Modify instead, so
// the parts they did not read are not rolled back.
func (s *Store) Save(p Preferences) error {
	return s.Modify(func(cur *Preferences) error {
		*cur = p
		return nil
	})
}

// TakeNotice returns, once, a plain-words message when a write moved a corrupt
// settings.json aside (naming where it went), and "" otherwise or after it was
// taken. It is per Store, so it covers writes made by this process only.
func (s *Store) TakeNotice() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.notice
	s.notice = ""
	return n
}

// Modify is the one read-modify-write of the document. It takes the in-process
// mutex and the cross-process lock file, reads the current document, applies fn
// and writes the result atomically (temp file + rename, 0600). If fn returns an
// error nothing is written. fn must not call back into the Store.
//
// A settings.json that does not parse is never overwritten silently: it is
// moved to settings.json.corrupt-<UTC time> (0600, same directory), fn starts
// from Defaults(), and TakeNotice reports where the old file went. A file that
// cannot be read at all (permissions, I/O) refuses the write.
func (s *Store) Modify(fn func(*Preferences) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}
	release, err := s.lock()
	if err != nil {
		return err
	}
	defer release()

	p, err := s.read()
	if err != nil {
		if !errors.Is(err, ErrCorrupt) {
			return fmt.Errorf("refusing to change preferences: %w", err)
		}
		moved, mvErr := s.moveAside()
		if mvErr != nil {
			return fmt.Errorf("refusing to overwrite a corrupt preferences file: %w", mvErr)
		}
		s.raise(fmt.Sprintf("Your settings file could not be read, so it was moved to %s and settings were reset to defaults. Fix or delete that copy; it is not used.", moved))
	}
	if err := fn(&p); err != nil {
		return err
	}
	return s.write(p.Normalize())
}

// lock takes the cross-process lock file, waiting up to the timeout for another
// process's write to finish, and returns its release func. The lock file is
// persistent (never unlinked), so every waiter locks the same inode. A
// transient error is retried until the deadline; an unsafe lock file (symlink,
// foreign owner, hard links) is refused at once.
func (s *Store) lock() (func(), error) {
	dir, err := os.Open(s.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open config dir: %w", err)
	}
	timeout := s.lockTimeout
	if timeout <= 0 {
		timeout = defaultLockTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		l, err := filelock.AcquirePersistentAt(dir, lockName)
		if err == nil {
			return func() { _ = l.Release(); _ = dir.Close() }, nil
		}
		if errors.Is(err, filelock.ErrUnsafe) {
			_ = dir.Close()
			return nil, fmt.Errorf("failed to lock preferences: %w", err)
		}
		if time.Now().After(deadline) {
			_ = dir.Close()
			return nil, fmt.Errorf("preferences are being written by another Slaw process (waited %s): %w", timeout, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// moveAside renames settings.json to a fresh settings.json.corrupt-<UTC time>
// beside it, keeping the person's bytes, and returns the new path.
func (s *Store) moveAside() (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dst := s.SettingsPath() + ".corrupt-" + stamp
	for i := 2; ; i++ {
		if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
			break
		}
		dst = fmt.Sprintf("%s.corrupt-%s-%d", s.SettingsPath(), stamp, i)
	}
	if err := os.Rename(s.SettingsPath(), dst); err != nil {
		return "", fmt.Errorf("failed to move the corrupt file aside: %w", err)
	}
	_ = os.Chmod(dst, 0o600) // best-effort: the bytes are the person's, keep them private
	return dst, nil
}

// write replaces the document atomically (temp file + rename) with 0600 perms.
// The caller holds the locks.
func (s *Store) write(p Preferences) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode preferences: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.dir, ".settings-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp preferences file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to write preferences: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to set preferences permissions: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("failed to sync preferences file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("failed to close preferences file: %w", err)
	}
	if err := os.Rename(tmpName, s.SettingsPath()); err != nil {
		cleanup()
		return fmt.Errorf("failed to replace preferences file: %w", err)
	}
	return nil
}

// EnsureThemesDir creates the themes directory (and a README explaining the
// format) so "Open themes folder" in Settings always lands somewhere real.
func (s *Store) EnsureThemesDir() (string, error) {
	dir := s.ThemesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create themes dir: %w", err)
	}
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); errors.Is(err, fs.ErrNotExist) {
		_ = os.WriteFile(readme, []byte(themesReadme), 0o600) // best-effort
	}
	return dir, nil
}

// ListUserThemes returns every *.json file in the themes directory, sorted by
// filename. Files that cannot be read, or exceed MaxThemeFileBytes, are
// reported in the second return value and otherwise skipped. A missing
// directory is not an error: it simply yields no themes.
func (s *Store) ListUserThemes() ([]ThemeFile, []string) {
	dir := s.ThemesDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{fmt.Sprintf("%s: %v", dir, err)}
	}

	var out []ThemeFile
	var problems []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(name), ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if info.Size() > MaxThemeFileBytes {
			problems = append(problems, fmt.Sprintf("%s: file is larger than %d KB", name, MaxThemeFileBytes/1024))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if !json.Valid(data) {
			problems = append(problems, fmt.Sprintf("%s: not valid JSON", name))
			continue
		}
		out = append(out, ThemeFile{Path: path, Filename: name, JSON: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename < out[j].Filename })
	return out, problems
}

const themesReadme = `# Slaw themes

Drop a *.json theme file in this folder and it appears under
Settings → Appearance the next time you press "Reload themes".

A theme file looks like:

    {
      "name": "My Theme",
      "type": "dark",
      "extends": "schoolhouse-dark",
      "colors": {
        "accent": "#7C9CFF",
        "bg": "#14161E"
      }
    }

"extends" names a built-in theme (schoolhouse-light, schoolhouse-dark,
signal, chalkboard) whose values fill in every key you leave out, so a theme
can be as small as one colour. Colours must be hex (#RGB, #RRGGBB or
#RRGGBBAA). The full key list is documented in docs/reference/themes.md
in the Slaw repository.
`
