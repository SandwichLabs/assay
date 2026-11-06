package prefs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RecentsKey is where the recent-workspaces list lives in the preferences
// document (Preferences.Extra), outside every workspace.
const RecentsKey = "recentWorkspaces"

// MaxRecents caps the launcher's list.
const MaxRecents = 12

// Recent is one remembered workspace folder.
type Recent struct {
	Path       string `json:"path"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	LastOpened string `json:"lastOpened"` // RFC 3339
}

// Recents is the launcher's list of recently opened workspaces, kept in the
// preferences document beside the theme. Every
// change goes through Store.Modify (an in-process mutex plus a cross-process
// file lock), so a stale copy round-tripped by the frontend, or another Slaw
// process, can never roll the list back.
//
// A nil *Recents, or one over a nil store, remembers nothing and lists nothing:
// preferences are optional (no user config dir).
type Recents struct {
	store  *Store
	logger *slog.Logger

	// base is the document this process last handed the frontend (Preferences)
	// or saved (SavePreferences); SavePreferences writes only the fields that
	// differ from it, so a window holding a stale copy cannot roll back another
	// window's change.
	baseMu  sync.Mutex
	base    Preferences
	baseSet bool
}

// NewRecents returns the recents list over store (which may be nil). logger may
// be nil.
func NewRecents(store *Store, logger *slog.Logger) *Recents {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Recents{store: store, logger: logger}
}

// SamePath reports whether a and b name the same folder: equal strings, or equal
// after resolving symlinks.
func SamePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

var errNoStore = fmt.Errorf("preferences are unavailable: no user config directory")

// has reports whether r can read and write preferences.
func (r *Recents) has() bool { return r != nil && r.store != nil }

// load reads the preferences for a read-only caller; a corrupt or unreadable
// file reads as the defaults and is logged. Writers use modify, never load.
func (r *Recents) load() Preferences {
	if !r.has() {
		return Defaults()
	}
	p, err := r.store.Load()
	if err != nil {
		r.logger.Warn("preferences unreadable, using defaults", "error", err)
	}
	return p
}

// modify is the only way Recents writes: Store.Modify, which holds the in-process
// and cross-process locks around read, change and write, and moves a corrupt
// file aside instead of overwriting it (TakeNotice says so).
func (r *Recents) modify(fn func(*Preferences) error) error {
	if !r.has() {
		return errNoStore
	}
	return r.store.Modify(func(p *Preferences) error {
		r.warnBadKeys(p)
		return fn(p)
	})
}

// warnBadKeys logs a warning for each of this package's sub-keys whose value
// has the wrong shape: it reads as empty, so the next write replaces it.
func (r *Recents) warnBadKeys(p *Preferences) {
	check := func(key string, into any) {
		if raw, ok := p.Extra[key]; ok && json.Unmarshal(raw, into) != nil {
			r.logger.Warn("preferences: unexpected value, it will be replaced on this write", "key", key)
		}
	}
	check(RecentsKey, &[]Recent{})
	check(UpdatesKey, &UpdateSettings{})
	check(MCPKey, &[]MCPEntry{})
}

// Preferences loads the document for the frontend and remembers it as the
// baseline for SavePreferences. A corrupt file reads as the defaults (the UI
// still starts) and raises the store's up-front notice; the error is returned
// for the caller to log.
func (r *Recents) Preferences() (Preferences, error) {
	if !r.has() {
		return Defaults(), errNoStore
	}
	p, err := r.store.Load()
	r.store.NoteUnreadable(err)
	r.baseMu.Lock()
	r.base, r.baseSet = p, true
	r.baseMu.Unlock()
	return p, err
}

// recentsFrom decodes the list from p; an undecodable list reads as empty.
func (r *Recents) recentsFrom(p *Preferences) []Recent {
	var recs []Recent
	if raw, ok := p.Extra[RecentsKey]; ok {
		if err := json.Unmarshal(raw, &recs); err != nil {
			r.logger.Warn("recent workspaces: list unreadable, starting over", "error", err)
			recs = nil
		}
	}
	return recs
}

// setRecents sorts, caps and stores recs in p.
func setRecents(p *Preferences, recs []Recent) error {
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].LastOpened > recs[j].LastOpened })
	if len(recs) > MaxRecents {
		recs = recs[:MaxRecents]
		pruneMCP(p, recs) // a workspace that fell off the list does not keep serving
	}
	raw, err := json.Marshal(recs)
	if err != nil {
		return fmt.Errorf("failed to encode the recent list: %w", err)
	}
	if p.Extra == nil {
		p.Extra = map[string]json.RawMessage{}
	}
	p.Extra[RecentsKey] = raw
	return nil
}

// List returns the list, most recent first. It only reads the preferences file.
func (r *Recents) List() []Recent {
	if r == nil {
		return nil
	}
	p := r.load()
	recs := r.recentsFrom(&p)
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].LastOpened > recs[j].LastOpened })
	return recs
}

// Record puts the workspace at root at the top of the list.
func (r *Recents) Record(root, id, name string) {
	if !r.has() {
		return
	}
	err := r.modify(func(p *Preferences) error {
		recs := r.recentsFrom(p)
		stamp := now()
		found := false
		for i := range recs {
			if SamePath(recs[i].Path, root) {
				recs[i].Path, recs[i].ID, recs[i].Name, recs[i].LastOpened = root, id, name, stamp
				found = true
			}
		}
		if !found {
			recs = append(recs, Recent{Path: root, ID: id, Name: name, LastOpened: stamp})
		}
		return setRecents(p, recs)
	})
	if err != nil {
		r.logger.Warn("failed to remember the workspace", "error", err)
	}
}

// Remove drops path from the list (never touching files) and returns the entry
// so the caller can offer Undo. nil when path was not listed.
func (r *Recents) Remove(path string) (*Recent, error) {
	if !r.has() {
		return nil, nil
	}
	var removed *Recent
	err := r.modify(func(p *Preferences) error {
		removed = nil
		recs := r.recentsFrom(p)
		kept := recs[:0:0]
		for _, e := range recs {
			if SamePath(e.Path, path) && removed == nil {
				e := e
				removed = &e
				continue
			}
			kept = append(kept, e)
		}
		if removed != nil {
			dropMCP(p, path) // a forgotten workspace must not start serving again
		}
		return setRecents(p, kept)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update the list: %w", err)
	}
	return removed, nil
}

// Restore puts back an entry Remove returned (Undo). A path already listed is
// left alone.
func (r *Recents) Restore(rec Recent) error {
	if strings.TrimSpace(rec.Path) == "" {
		return fmt.Errorf("nothing to restore")
	}
	if !r.has() {
		return nil
	}
	return r.modify(func(p *Preferences) error {
		recs := r.recentsFrom(p)
		for _, e := range recs {
			if SamePath(e.Path, rec.Path) {
				return nil
			}
		}
		if rec.LastOpened == "" {
			rec.LastOpened = now()
		}
		return setRecents(p, append(recs, rec))
	})
}

// Relocate re-points the entry at oldPath at newRoot.
func (r *Recents) Relocate(oldPath, newRoot string) error {
	if !r.has() {
		return nil
	}
	return r.modify(func(p *Preferences) error {
		recs := r.recentsFrom(p)
		for i := range recs {
			if SamePath(recs[i].Path, oldPath) || recs[i].Path == oldPath {
				recs[i].Path = newRoot
			}
		}
		moveMCP(p, oldPath, newRoot)
		return setRecents(p, recs)
	})
}

// IDs maps every remembered folder to its workspace id (entries without one are
// left out).
func (r *Recents) IDs() map[string]string {
	out := map[string]string{}
	for _, e := range r.List() {
		if e.ID != "" {
			out[e.Path] = e.ID
		}
	}
	return out
}

// SavePreferences saves p, but writes only the known fields that differ from the
// copy this process last handed the frontend or saved, onto what is on disk
// now. The frontend round-trips its whole state, loaded once at startup, so
// writing it back whole would roll back another window's theme or scale change;
// unknown and package-owned keys (the recent list, update and MCP settings, keys
// from a newer build) are never taken from p. With no baseline yet, every known
// field is written. The error is "unavailable" when there is no store.
func (r *Recents) SavePreferences(p Preferences) error {
	p = p.Normalize()
	r.baseMu.Lock()
	base, have := r.base, r.baseSet
	r.baseMu.Unlock()
	err := r.modify(func(cur *Preferences) error {
		mergeChanged(cur, base, p, have)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save preferences: %w", err)
	}
	r.baseMu.Lock()
	r.base, r.baseSet = p, true
	r.baseMu.Unlock()
	return nil
}

// mergeChanged copies into cur each known field of next that differs from base
// (all of them when haveBase is false).
func mergeChanged(cur *Preferences, base, next Preferences, haveBase bool) {
	if !haveBase || next.Theme != base.Theme {
		cur.Theme = next.Theme
	}
	if !haveBase || next.SystemLight != base.SystemLight {
		cur.SystemLight = next.SystemLight
	}
	if !haveBase || next.SystemDark != base.SystemDark {
		cur.SystemDark = next.SystemDark
	}
	if !haveBase || next.ReduceMotion != base.ReduceMotion {
		cur.ReduceMotion = next.ReduceMotion
	}
	if !haveBase || next.UIScale != base.UIScale {
		cur.UIScale = next.UIScale
	}
}
