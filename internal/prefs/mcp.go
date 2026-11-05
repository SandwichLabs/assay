package prefs

import (
	"encoding/json"
	"fmt"
)

// MCPKey is where the workspaces whose Studio MCP server is switched on live in
// the preferences document (Preferences.Extra). Per person and per machine:
// never part of a workspace, slaw.yaml or a bundle.
const MCPKey = "mcpWorkspaces"

// MCPEntry is one workspace left on. It is keyed by path and workspace id, like
// the recents list, so a different workspace later made or cloned at the same
// path does not start serving by itself.
type MCPEntry struct {
	Path string `json:"path"`
	ID   string `json:"id"`
}

func mcpFrom(p Preferences) []MCPEntry {
	var entries []MCPEntry
	if raw, ok := p.Extra[MCPKey]; ok {
		_ = json.Unmarshal(raw, &entries) // a bad value (or the old shape) reads as "none on"
	}
	return entries
}

func setMCP(p *Preferences, entries []MCPEntry) error {
	if entries == nil {
		entries = []MCPEntry{}
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	if p.Extra == nil {
		p.Extra = map[string]json.RawMessage{}
	}
	p.Extra[MCPKey] = raw
	return nil
}

// dropMCP removes the entries for path from p (no save).
func dropMCP(p *Preferences, path string) {
	if _, ok := p.Extra[MCPKey]; !ok {
		return
	}
	var kept []MCPEntry
	for _, e := range mcpFrom(*p) {
		if !SamePath(e.Path, path) {
			kept = append(kept, e)
		}
	}
	_ = setMCP(p, kept)
}

// moveMCP re-points the entries at oldPath to newRoot (no save).
func moveMCP(p *Preferences, oldPath, newRoot string) {
	if _, ok := p.Extra[MCPKey]; !ok {
		return
	}
	entries := mcpFrom(*p)
	for i := range entries {
		if SamePath(entries[i].Path, oldPath) {
			entries[i].Path = newRoot
		}
	}
	_ = setMCP(p, entries)
}

// MCPOn reports whether the person left Studio's MCP server on for the
// workspace at root with this id. A workspace never switched on (or off), or a
// different workspace at the same path, reads false.
func (r *Recents) MCPOn(root, id string) bool {
	if r == nil || r.store == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, _ := r.loadLocked()
	for _, e := range mcpFrom(p) {
		if e.ID == id && SamePath(e.Path, root) {
			return true
		}
	}
	return false
}

// SetMCPOn remembers the on/off choice for the workspace at root. Off removes
// the entry, so the list holds only workspaces that come back on.
func (r *Recents) SetMCPOn(root, id string, on bool) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("preferences are unavailable: no user config directory")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, _ := r.loadLocked()
	if _, ok := p.Extra[MCPKey]; !ok {
		if err := setMCP(&p, nil); err != nil {
			return err
		}
	}
	dropMCP(&p, root)
	if on {
		if err := setMCP(&p, append(mcpFrom(p), MCPEntry{Path: root, ID: id})); err != nil {
			return err
		}
	}
	if err := r.store.Save(p); err != nil {
		return fmt.Errorf("failed to save the MCP choice: %w", err)
	}
	return nil
}
