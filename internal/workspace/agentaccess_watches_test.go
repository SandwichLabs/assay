package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/watches"
)

// The agent can write a watch file but is handed the history read-only: it
// cannot be turned back into the appender, so no tool can record a check.
func TestAgentAccess_WatchHistoryIsReadOnly(t *testing.T) {
	ws, _ := newWorkspace(t)
	a := NewAgentAccess(ws, nil)
	var hist any = a.WatchHistory()
	_, ok := hist.(watches.History)
	assert.False(t, ok, "no Append in the agent's history")
	_, ok = hist.(interface {
		Append(context.Context, watches.Entry) error
	})
	assert.False(t, ok)
	require.NotNil(t, a.Watches())

	entries, err := a.WatchHistory().Entries(context.Background(), "")
	require.NoError(t, err)
	assert.Empty(t, entries)
}
