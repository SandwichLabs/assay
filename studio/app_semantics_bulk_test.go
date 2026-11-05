package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/semantics/proposals"
)

func termDraft(term string) proposals.Draft {
	return proposals.Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": term, "text": term + " is x"}}
}

func TestAcceptProposals_AcceptsTheShownPinsAndRefreshes(t *testing.T) {
	app := newOpenApp(t)
	ctx := context.Background()
	require.NoError(t, app.ws.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Base", SQL: "SELECT 'east' AS region, 10 AS amount"}))
	var events []string
	app.emit = func(name string, _ ...any) { events = append(events, name) }

	m := fileProposal(t, app, netRevenueDraft(""))
	g1 := fileProposal(t, app, termDraft("Churn"), time.Second)
	g2 := fileProposal(t, app, termDraft("Retention"), 2*time.Second)
	var pins []ProposalPin
	for _, id := range []string{m.ID, g1.ID, g2.ID} {
		pv, err := app.GetProposal(id)
		require.NoError(t, err)
		pins = append(pins, ProposalPin{ID: id, ShownRev: pv.ShownRev})
	}

	res, err := app.AcceptProposals(pins)
	require.NoError(t, err)
	assert.Empty(t, res.Error)
	assert.Empty(t, res.FailedID)
	require.Len(t, res.Accepted, 3)
	assert.Equal(t, "semantics/metrics/net_revenue.yaml", res.Accepted[0].Target)
	assert.Contains(t, events, "data:changed")
	list, err := app.ListProposals(false)
	require.NoError(t, err)
	assert.Empty(t, list)
	g, err := app.ws.Semantics().Glossary(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"Churn", "Retention"}, g.Terms)
}

func TestAcceptProposals_StopsAtTheFirstRefusalAndReportsIt(t *testing.T) {
	app := newOpenApp(t)
	ctx := context.Background()
	a := fileProposal(t, app, termDraft("Churn"))
	b := fileProposal(t, app, netRevenueDraft(""), time.Second)
	c := fileProposal(t, app, termDraft("Retention"), 2*time.Second)
	var pins []ProposalPin
	for _, id := range []string{a.ID, b.ID, c.ID} {
		pv, err := app.GetProposal(id)
		require.NoError(t, err)
		pins = append(pins, ProposalPin{ID: id, ShownRev: pv.ShownRev})
	}
	// the metric's target appears after the person looked
	m, err := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(z)\n"))
	require.NoError(t, err)
	require.NoError(t, app.ws.Semantics().PutMetric(ctx, m))

	res, err := app.AcceptProposals(pins)
	require.NoError(t, err, "a refusal is a result, not a binding error")
	assert.Equal(t, b.ID, res.FailedID)
	assert.Contains(t, res.Error, "changed again since you looked")
	require.Len(t, res.Accepted, 1)
	assert.Equal(t, a.ID, res.Accepted[0].ID)
	cur, err := app.ws.Semantics().Metric(ctx, "net_revenue")
	require.NoError(t, err)
	assert.Equal(t, "sum(z)", cur.Measure, "never overwritten")
	open, err := app.ListProposals(false)
	require.NoError(t, err)
	assert.Len(t, open, 2, "the refused one and the one after it stay open")
}

func TestAcceptProposals_NeedsPinsAndAWorkspace(t *testing.T) {
	app := newOpenApp(t)
	p := fileProposal(t, app, termDraft("Churn"))
	res, err := app.AcceptProposals([]ProposalPin{{ID: p.ID}})
	require.NoError(t, err)
	assert.Equal(t, p.ID, res.FailedID)
	assert.Contains(t, res.Error, "needs the rev of the version you looked at")
	assert.Empty(t, res.Accepted)

	res, err = app.AcceptProposals(nil)
	require.NoError(t, err)
	assert.Empty(t, res.Accepted)
	assert.Empty(t, res.Error)

	closed := &App{}
	_, err = closed.AcceptProposals(nil)
	assert.Error(t, err)
}

func TestAcceptProposals_RefusesAProposalFileEditedAfterItWasRead(t *testing.T) {
	app := newOpenApp(t)
	g := fileProposal(t, app, termDraft("Churn"))
	pv, err := app.GetProposal(g.ID)
	require.NoError(t, err)

	// the proposal file itself is edited by hand between the read and the accept
	path := filepath.Join(app.ws.Root(), ".slaw-local", "proposals", g.ID+".yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := strings.Replace(string(raw), "Churn is x", "Churn is something else", 1)
	require.NotEqual(t, string(raw), edited)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o600))

	res, err := app.AcceptProposals([]ProposalPin{{ID: g.ID, ShownRev: pv.ShownRev}})
	require.NoError(t, err)
	assert.Equal(t, g.ID, res.FailedID)
	assert.Contains(t, res.Error, "changed again since you looked")
	assert.Empty(t, res.Accepted)
	g2, err := app.ws.Semantics().Glossary(context.Background())
	require.NoError(t, err)
	assert.Empty(t, g2.Terms, "nothing was written")
}
