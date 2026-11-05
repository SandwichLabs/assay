package prefs

import (
	"encoding/json"
	"fmt"
)

// MCPKey is where the workspaces whose Studio MCP server is switched on live in
// the preferences document (Preferences.Extra). Per person and per machine:
// never part of a workspace, slaw.yaml or a bundle.
const MCPKey = "mcpWorkspaces"

func mcpFrom(p Preferences) []string {
	var paths []string
	if raw, ok := p.Extra[MCPKey]; ok {
		_ = json.Unmarshal(raw, &paths) // a bad value reads as "none on"
	}
	return paths
}

// MCPOn reports whether the person left Studio's MCP server on for the
// workspace at root. A workspace never switched on (or off) reads false.
func (r *Recents) MCPOn(root string) bool {
	if r == nil || r.store == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, _ := r.loadLocked()
	for _, e := range mcpFrom(p) {
		if SamePath(e, root) {
			return true
		}
	}
	return false
}

// SetMCPOn remembers the on/off choice for the workspace at root. Off removes
// the entry, so the list holds only workspaces that come back on.
func (r *Recents) SetMCPOn(root string, on bool) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("preferences are unavailable: no user config directory")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, _ := r.loadLocked()
	kept := []string{}
	for _, e := range mcpFrom(p) {
		if !SamePath(e, root) {
			kept = append(kept, e)
		}
	}
	if on {
		kept = append(kept, root)
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	if p.Extra == nil {
		p.Extra = map[string]json.RawMessage{}
	}
	p.Extra[MCPKey] = raw
	if err := r.store.Save(p); err != nil {
		return fmt.Errorf("failed to save the MCP choice: %w", err)
	}
	return nil
}
