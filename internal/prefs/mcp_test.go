package prefs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPOnRemembersPerWorkspace(t *testing.T) {
	r, _ := newTestRecents(t)
	a, b := t.TempDir(), t.TempDir()
	assert.False(t, r.MCPOn(a), "never set reads off")

	require.NoError(t, r.SetMCPOn(a, true))
	assert.True(t, r.MCPOn(a))
	assert.False(t, r.MCPOn(b), "another workspace is unaffected")

	require.NoError(t, r.SetMCPOn(a, false))
	assert.False(t, r.MCPOn(a))
}

func TestMCPOnSurvivesSavePreferences(t *testing.T) {
	r, _ := newTestRecents(t)
	a := t.TempDir()
	require.NoError(t, r.SetMCPOn(a, true))
	require.NoError(t, r.SavePreferences(Defaults()), "a stale copy from the frontend")
	assert.True(t, r.MCPOn(a))
}

func TestMCPOnWithoutStore(t *testing.T) {
	var r *Recents
	assert.False(t, r.MCPOn("/x"))
	assert.Error(t, r.SetMCPOn("/x", true))
}
