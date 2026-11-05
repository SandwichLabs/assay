package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/agents"
)

func proposeTerm(t *testing.T, root, term string) string {
	t.Helper()
	resp := agentCall(t, root, agents.ModeFull, "propose_definition", map[string]any{
		"kind": "glossary", "rationale": "Used both ways in the thread.",
		"definition": map[string]any{"term": term, "text": term + " means something."},
	})
	require.Nil(t, resp.Error, "%+v", resp.Error)
	id, _ := resp.Result.(map[string]any)["id"].(string)
	require.NotEmpty(t, id)
	return id
}

func TestSemanticsAcceptAll_YesAcceptsEveryOpenProposal(t *testing.T) {
	root := proposalWorkspace(t)
	m := proposeNetRevenue(t, root, nil)
	g1, g2 := proposeTerm(t, root, "Churn"), proposeTerm(t, root, "Retention")

	out, stderr, err := runSlaw("semantics", "accept", "--all", "--yes", "--workspace", root)
	require.NoError(t, err, stderr)
	for _, id := range []string{m, g1, g2} {
		assert.Contains(t, out, "Accepted "+id)
		assert.Contains(t, stderr, "Proposal  "+id, "each diff is shown")
	}
	_, err = os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, err)
	g, err := os.ReadFile(filepath.Join(root, "semantics", "glossary.md"))
	require.NoError(t, err)
	assert.Contains(t, string(g), "## Churn")
	assert.Contains(t, string(g), "## Retention")

	out, _, err = runSlaw("semantics", "accept", "--all", "--yes", "--workspace", root)
	require.NoError(t, err)
	assert.Contains(t, out, "No proposals are waiting")
}

func TestSemanticsAcceptAll_KindFilter(t *testing.T) {
	root := proposalWorkspace(t)
	m := proposeNetRevenue(t, root, nil)
	g := proposeTerm(t, root, "Churn")
	out, stderr, err := runSlaw("semantics", "accept", "--all", "--kind", "glossary", "--yes", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Accepted "+g)
	assert.NotContains(t, out, m)
	_, err = os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.True(t, os.IsNotExist(err), "the metric proposal stays open")

	_, _, err = runSlaw("semantics", "accept", "--all", "--kind", "bogus", "--yes", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --kind")
}

func TestSemanticsAcceptAll_NeedsATerminalWithoutYes(t *testing.T) {
	root := proposalWorkspace(t)
	proposeNetRevenue(t, root, nil)
	_, _, err := runSlaw("semantics", "accept", "--all", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a terminal")
	_, statErr := os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestSemanticsAcceptAll_ConfirmationPrompt(t *testing.T) {
	old := acceptTTY
	acceptTTY = func() bool { return true }
	t.Cleanup(func() { acceptTTY = old })
	root := proposalWorkspace(t)
	proposeNetRevenue(t, root, nil)
	target := filepath.Join(root, "semantics", "metrics", "net_revenue.yaml")

	_, stderr, err := runSlawIn(strings.NewReader("n\n"), "semantics", "accept", "--all", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, stderr, "[y/N]")
	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr), "declining writes nothing")

	out, stderr, err := runSlawIn(strings.NewReader("y\n"), "semantics", "accept", "--all", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "Accepted ")
	_, statErr = os.Stat(target)
	assert.NoError(t, statErr)
}

func TestSemanticsAcceptAll_MutuallyExclusiveFlags(t *testing.T) {
	root := proposalWorkspace(t)
	id := proposeNetRevenue(t, root, nil)
	for _, args := range [][]string{
		{"semantics", "accept", id, "--all", "--yes"},
		{"semantics", "accept", "--all", "--yes", "--overwrite"},
		{"semantics", "accept", "--all", "--yes", "--rev", "abc"},
	} {
		_, _, err := runSlaw(append(args, "--workspace", root)...)
		require.Error(t, err, args)
	}
	_, _, err := runSlaw("semantics", "accept", "--workspace", root)
	require.Error(t, err, "no id and no --all")
	_, statErr := os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestSemanticsAcceptAll_StopsWhenOneProposalForTheSameFileFollowsAnother(t *testing.T) {
	root := proposalWorkspace(t)
	// Two proposals for one file: whichever runs second is refused once the
	// first is written, and is not written over it.
	a := proposeNetRevenue(t, root, nil)
	b := proposeNetRevenue(t, root, map[string]any{
		"definition": map[string]any{"query": "net_revenue_base", "measure": "sum(amount * 2)", "dimensions": []any{"region"}},
	})
	require.NotEqual(t, a, b)

	out, _, err := runSlaw("semantics", "accept", "--all", "--yes", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 accepted and 0 not tried")
	assert.Contains(t, err.Error(), "changed again since you looked")
	assert.Equal(t, 1, strings.Count(out, "Accepted "), out)
	failed := a
	if strings.Contains(out, "Accepted "+a) {
		failed = b
	}
	assert.Contains(t, err.Error(), "stopped at "+failed)
	written, rerr := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, rerr)
	assert.Equal(t, strings.Contains(out, "Accepted "+b), strings.Contains(string(written), "amount * 2"), "the file holds the one that was accepted")
}

func TestSemanticsAcceptAll_StaleAtDisplayStopsWithTheStaleText(t *testing.T) {
	root := proposalWorkspace(t)
	id := proposeNetRevenue(t, root, nil)
	seedMetricFile(t, root, "net_revenue", cliMetric) // created after it was proposed: the proposal is stale
	before, rerr := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, rerr)

	out, stderr, err := runSlaw("semantics", "accept", "--all", "--yes", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped at "+id)
	assert.Contains(t, err.Error(), "was created since this was proposed")
	assert.Contains(t, stderr, "WARNING")
	assert.NotContains(t, out, "Accepted")
	after, rerr := os.ReadFile(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, rerr)
	assert.Equal(t, string(before), string(after), "bulk accept never overwrites")
}
