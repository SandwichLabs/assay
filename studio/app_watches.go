package main

import (
	"time"

	"github.com/SandwichLabs/slaw/internal/session"
	"github.com/SandwichLabs/slaw/internal/watches"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

// --- Watches (E7 W2): the Watches view ---
//
// A watch is a standing question with a condition, a file under watches/. The
// view lists them, checks them when the person asks and edits the files. A
// check goes through the same read-only handler as the Semantics view's runs
// (the agent policy's read-only path in the tightest tier), is registered under
// runKeyWatches so CancelRun stops it, and is never an agent tool or an MCP
// method. The rules live in internal/watches and internal/workspace; the
// shaping is in watches.go.

// runKeyWatches names a check in flight for CancelRun.
const runKeyWatches = "watches"

// ListWatches returns every file under watches/ (a file that cannot be read is
// listed with its error), each with its last check from the machine-local
// history, and how many watches' newest check fired.
func (a *App) ListWatches() (*WatchList, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return listWatches(a.ctxOrBackground(), a.ws)
}

// GetWatch opens one watch with its file as stored and its revision.
func (a *App) GetWatch(name string) (*WatchDetail, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return getWatch(a.ctxOrBackground(), a.ws, name)
}

// WatchHistory returns the recorded checks of one watch, newest first; limit 0
// returns all (the history keeps the newest 200).
func (a *App) WatchHistory(name string, limit int) ([]watches.Entry, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	return watchHistory(a.ctxOrBackground(), a.ws, name, limit)
}

// CheckWatches checks the named watches (none named: all of them) one at a
// time and records each outcome in the history. The results carry the sample
// rows of an alerting "rows" check, which exist only here. CancelRun("watches")
// stops it; the results so far come back with Cancelled set.
func (a *App) CheckWatches(names []string) (*WatchCheckRun, error) {
	h, err := a.definitionRunner()
	if err != nil {
		return nil, err
	}
	ctx, end := a.runs.begin(a.ctxOrBackground(), runKeyWatches)
	defer end()
	before := session.StatusSnapshot(h.sess)
	results, err := workspace.CheckWatches(ctx, h.ws, names, h, time.Now())
	a.afterReadOnlyRun(h.sess, before)
	a.emitEvent("watches:checked")
	return watchCheckRun(ctx, results, err)
}

// PreviewWatch returns the file a draft would write, or why it would not be
// written. Nothing is saved.
func (a *App) PreviewWatch(d WatchDraft) (string, error) {
	return previewWatch(d)
}

// SaveWatch writes a watch against the revision the form loaded and returns the
// new revision. A file that changed since is a conflict (Overwrite is "Keep
// mine"); a draft the decoder refuses comes back with its reason.
func (a *App) SaveWatch(d WatchDraft) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return "", err
	}
	rev, err := saveWatch(a.ctxOrBackground(), a.ws.Watches(), d)
	if err != nil {
		return "", err
	}
	a.emitEvent("data:changed")
	return rev, nil
}

// DeleteWatch removes a watch's file if it is still at rev; its history stays.
func (a *App) DeleteWatch(name, rev string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if err := a.requireWorkspace(); err != nil {
		return err
	}
	if err := deleteWatch(a.ctxOrBackground(), a.ws.Watches(), name, rev); err != nil {
		return err
	}
	a.emitEvent("data:changed")
	return nil
}
