package prefs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPOnRemembersPerWorkspace(t *testing.T) {
	r, _ := newTestRecents(t)
	a, b := t.TempDir(), t.TempDir()
	assert.False(t, r.MCPOn(a, "ida"), "never set reads off")

	require.NoError(t, r.SetMCPOn(a, "ida", true))
	assert.True(t, r.MCPOn(a, "ida"))
	assert.False(t, r.MCPOn(b, "idb"), "another workspace is unaffected")

	require.NoError(t, r.SetMCPOn(a, "ida", false))
	assert.False(t, r.MCPOn(a, "ida"))
}

func TestMCPOnIsKeyedByWorkspaceID(t *testing.T) {
	r, _ := newTestRecents(t)
	a := t.TempDir()
	require.NoError(t, r.SetMCPOn(a, "ida", true))
	assert.False(t, r.MCPOn(a, "other"), "a different workspace at the same path must not auto-start")
}

func TestMCPOnSurvivesSavePreferences(t *testing.T) {
	r, _ := newTestRecents(t)
	a := t.TempDir()
	require.NoError(t, r.SetMCPOn(a, "ida", true))
	require.NoError(t, r.SavePreferences(Defaults()), "a stale copy from the frontend")
	assert.True(t, r.MCPOn(a, "ida"))
}

func TestMCPOnFollowsRecentsRemoveAndRelocate(t *testing.T) {
	r, _ := newTestRecents(t)
	a, b := t.TempDir(), t.TempDir()
	r.Record(a, "ida", "A")
	require.NoError(t, r.SetMCPOn(a, "ida", true))

	require.NoError(t, r.Relocate(a, b))
	assert.False(t, r.MCPOn(a, "ida"))
	assert.True(t, r.MCPOn(b, "ida"), "the entry follows the moved folder")

	_, err := r.Remove(b)
	require.NoError(t, err)
	assert.False(t, r.MCPOn(b, "ida"), "forgetting a workspace drops its choice")
	require.NoError(t, r.Restore(Recent{Path: b, ID: "ida", Name: "A"}))
	assert.False(t, r.MCPOn(b, "ida"), "undo does not bring the server back on")
}

func TestMCPOnWithoutStore(t *testing.T) {
	var r *Recents
	assert.False(t, r.MCPOn("/x", "id"))
	assert.Error(t, r.SetMCPOn("/x", "id", true))
}
