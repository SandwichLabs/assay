package main

// Studio's own MCP switch.

import (
	"errors"
	"fmt"

	"github.com/SandwichLabs/slaw/internal/agents"
	"github.com/SandwichLabs/slaw/internal/hub"
	slawmcp "github.com/SandwichLabs/slaw/internal/mcp"
)

// MCPStatus is Studio's own MCP switch. It never carries the bearer token.
type MCPStatus struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	// CanStart is false when this window cannot serve MCP (read-only, or no
	// workspace); Reason says why.
	CanStart bool   `json:"canStart"`
	Reason   string `json:"reason"`
	// StartError is why the server did not come back when the workspace opened
	// (it was left on); empty otherwise.
	StartError string `json:"startError"`
	// Remembered is whether the server is on for this workspace on this machine
	// (it starts again when the workspace opens), even in a read-only window
	// that cannot serve it now.
	Remembered bool `json:"remembered"`
	// SaveError is why the on/off choice could not be remembered; empty when it
	// was saved.
	SaveError string `json:"saveError"`
}

// StartMCP serves this workspace's agent tools over MCP on a loopback port and
// publishes the endpoint in hub.lock, so `slaw mcp serve` (what Claude Desktop
// and Claude Code launch) proxies to Studio. A write that needs approval shows
// the same approval card the chat agent uses. The choice is remembered for this
// workspace on this machine, and the server comes back when it is opened again.
func (a *App) StartMCP() (*MCPStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.requireWorkspace(); err != nil {
		return nil, err
	}
	if a.readOnly || a.hold == nil {
		return nil, errors.New("This window opened the workspace read-only, so it can't serve MCP. Take over the workspace first.")
	}
	if err := a.startMCPLocked(); err != nil {
		return nil, err
	}
	a.rememberMCPLocked(true)
	a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
	return a.mcpStatusLocked(), nil
}

// startMCPLocked starts the server if it is not running. The caller holds a.mu
// and has checked that the workspace is open and not read-only.
func (a *App) startMCPLocked() error {
	if a.wmcp != nil {
		return nil
	}
	h := a.newAgentToolHandler(agents.ModeFull)
	h.enableApprovals("mcp")
	m, err := hub.StartWorkspaceMCP(a.ws, a.sess, a.hold,
		hub.WithMCPLogger(a.logger),
		hub.WithMCPRememberPort(),
		hub.WithMCPVersion(version),
		hub.WithMCPPolicyRefresh(h.refreshPolicy),
		hub.WithMCPHandler(h.inner),
		hub.WithMCPHandlerWrap(func(slawmcp.Handler) slawmcp.Handler { return h }),
	)
	if err != nil {
		return fmt.Errorf("failed to start the MCP server: %w", err)
	}
	a.wmcp, a.mcpTool, a.mcpStartErr = m, h, ""
	a.logger.Info("MCP server started", "url", m.URL())
	return nil
}

// restoreMCPLocked runs when a workspace opens: it brings the server back if the
// person left it on here. A failure never blocks the open; it is logged and
// shown in the MCP status, and the remembered choice stays on.
func (a *App) restoreMCPLocked() {
	a.mcpStartErr, a.mcpSaveErr, a.mcpOn = "", "", false
	if a.ws == nil {
		return
	}
	a.mcpOn = a.recents.MCPOn(a.ws.Root(), a.ws.ID())
	if !a.mcpOn || a.readOnly || a.hold == nil {
		return
	}
	start := a.startMCPLocked
	if a.mcpStarter != nil {
		start = a.mcpStarter
	}
	if err := start(); err != nil {
		a.mcpStartErr = err.Error()
		a.logger.Warn("MCP server did not restart when the workspace opened", "error", err)
	}
}

// rememberMCPLocked records the on/off choice for the open workspace. When it
// cannot be saved (no config dir), the status says so instead of claiming it is
// remembered.
func (a *App) rememberMCPLocked(on bool) {
	if a.ws == nil {
		return
	}
	a.mcpOn, a.mcpSaveErr = false, ""
	if err := a.recents.SetMCPOn(a.ws.Root(), a.ws.ID(), on); err != nil {
		a.mcpSaveErr = err.Error()
		a.logger.Warn("failed to remember the MCP choice", "error", err)
		return
	}
	a.mcpOn = on
}

// StopMCP turns Studio's MCP server off, clears the endpoint from hub.lock and
// remembers that it stays off for this workspace.
func (a *App) StopMCP() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rememberMCPLocked(false)
	a.mcpStartErr = ""
	if a.wmcp == nil {
		a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
		return nil
	}
	err := a.wmcp.Stop()
	a.wmcp, a.mcpTool = nil, nil
	a.emitEvent("workspace:changed", map[string]any{"kind": "mcp"})
	return err
}

// GetMCPStatus reports whether Studio's MCP server is running and where. It
// never includes the bearer token.
func (a *App) GetMCPStatus() *MCPStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mcpStatusLocked()
}

func (a *App) mcpStatusLocked() *MCPStatus {
	st := &MCPStatus{CanStart: a.ws != nil && !a.readOnly && a.hold != nil}
	switch {
	case a.ws == nil:
		st.Reason = "No workspace is open."
	case a.readOnly:
		st.Reason = "This window opened the workspace read-only. Take over the workspace to serve MCP."
	}
	st.StartError = a.mcpStartErr
	st.Remembered, st.SaveError = a.mcpOn, a.mcpSaveErr
	if a.wmcp != nil {
		st.Running, st.URL = true, a.wmcp.URL()
	}
	return st
}

// GetClaudeConfig returns the two snippets "Copy Claude Desktop config" and
// "Copy Claude Code config" put on the clipboard. Neither contains a token: they
// launch `slaw mcp serve --workspace DIR`, which reads the endpoint from
// hub.lock when Studio owns the workspace.
func (a *App) GetClaudeConfig() (*hub.ClaudeConfig, error) {
	a.mu.RLock()
	root := ""
	if a.ws != nil {
		root = a.ws.Root()
	}
	a.mu.RUnlock()
	if root == "" {
		return nil, errNoWorkspace
	}
	locate := a.locateSlaw
	if locate == nil {
		locate = hub.LocateSlaw
	}
	exe, err := locate()
	if err != nil {
		return nil, errors.New("Couldn't find the `slaw` command-line program. Install it, or put it on your PATH, then try again.")
	}
	cfg, err := hub.BuildClaudeConfig(root, exe)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
