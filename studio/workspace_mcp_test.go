package main

// #96: the MCP switch is remembered per workspace and comes back on open.

import (
	"errors"
	"testing"

	"github.com/SandwichLabs/slaw/internal/hub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPNeverSetDoesNotStartOnOpen(t *testing.T) {
	app := newOpenApp(t)
	assert.False(t, app.GetMCPStatus().Running)
	reopenApp(t, app)
	st := app.GetMCPStatus()
	assert.False(t, st.Running)
	assert.False(t, st.Remembered)
}

func TestMCPRememberedOnRestartsOnOpen(t *testing.T) {
	app := newOpenApp(t)
	st, err := app.StartMCP()
	require.NoError(t, err)
	require.True(t, st.Running)
	assert.True(t, st.Remembered)
	root := app.ws.Root()

	// Closing the workspace stops the server but must not forget the choice.
	reopenApp(t, app)
	st = app.GetMCPStatus()
	assert.True(t, st.Running, "the server comes back when the workspace is opened again")
	assert.Empty(t, st.StartError)
	assert.True(t, st.Remembered)

	// What `slaw mcp serve` proxies to: the endpoint is published in hub.lock.
	holder, err := hub.ReadHolder(root)
	require.NoError(t, err)
	require.NotNil(t, holder.MCP, "hub.lock advertises the MCP endpoint")
	assert.NotEmpty(t, holder.MCP.URL)
}

func TestMCPStopPersistsOff(t *testing.T) {
	app := newOpenApp(t)
	_, err := app.StartMCP()
	require.NoError(t, err)
	require.NoError(t, app.StopMCP())
	assert.False(t, app.GetMCPStatus().Remembered)

	reopenApp(t, app)
	assert.False(t, app.GetMCPStatus().Running, "a stopped server does not come back")
	assert.False(t, app.GetMCPStatus().Remembered)
}

func TestMCPFailedRestartDoesNotFailTheOpen(t *testing.T) {
	app := newOpenApp(t)
	_, err := app.StartMCP()
	require.NoError(t, err)
	app.mcpStarter = func() error { return errors.New("port busy") }

	root := app.ws.Root()
	require.NoError(t, app.CloseWorkspace())
	res, err := app.OpenWorkspace(root)
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)

	st := app.GetMCPStatus()
	assert.False(t, st.Running)
	assert.Contains(t, st.StartError, "port busy")
	assert.True(t, st.Remembered, "a failed restart keeps the choice")
}

func TestMCPReadOnlyWindowDoesNotStartButKeepsTheChoice(t *testing.T) {
	app := newOpenApp(t)
	_, err := app.StartMCP()
	require.NoError(t, err)
	root := app.ws.Root()
	require.NoError(t, app.CloseWorkspace())

	res, err := app.OpenWorkspaceReadOnly(root)
	require.NoError(t, err)
	require.True(t, res.Info.ReadOnly)

	st := app.GetMCPStatus()
	assert.False(t, st.Running, "a read-only window cannot serve MCP")
	assert.False(t, st.CanStart)
	assert.True(t, st.Remembered, "the choice is kept for the next full open")
	assert.Empty(t, st.StartError)
}

func TestMCPSaveFailureIsNotClaimedAsRemembered(t *testing.T) {
	app := newOpenApp(t)
	app.recents = nil // no config dir: preferences unavailable
	st, err := app.StartMCP()
	require.NoError(t, err)
	assert.True(t, st.Running)
	assert.False(t, st.Remembered)
	assert.NotEmpty(t, st.SaveError)
}
