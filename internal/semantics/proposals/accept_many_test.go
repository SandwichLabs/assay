package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/semantics"
)

// pinsOf shows each proposal, as the person's screen would, and pins it.
func pinsOf(t *testing.T, props Store, sem semantics.Store, ids ...string) []Pin {
	t.Helper()
	pins := make([]Pin, 0, len(ids))
	for _, id := range ids {
		pv, err := Show(context.Background(), props, sem, id)
		require.NoError(t, err)
		pins = append(pins, Pin{ID: id, ShownRev: pv.ShownRev})
	}
	return pins
}

func fileMetric(t *testing.T, props Store, name, measure string) *Proposal {
	t.Helper()
	d := metricDraft()
	d.Name = name
	defOf(&d)["measure"] = measure
	p := mustBuild(t, d)
	require.NoError(t, props.Create(context.Background(), p))
	return p
}

func fileTerm(t *testing.T, props Store, term string) *Proposal {
	t.Helper()
	p := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": term, "text": "x"}})
	require.NoError(t, props.Create(context.Background(), p))
	return p
}

func TestAcceptMany_AcceptsAllInOrder(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, sem := c.new(t)
			a := fileMetric(t, props, "alpha", "sum(a)")
			b := fileMetric(t, props, "beta", "sum(b)")
			res := AcceptMany(ctx, props, sem, pinsOf(t, props, sem, a.ID, b.ID))
			require.NoError(t, res.Err)
			assert.Empty(t, res.FailedID)
			require.Len(t, res.Accepted, 2)
			assert.Equal(t, []string{a.ID, b.ID}, []string{res.Accepted[0].ID, res.Accepted[1].ID})
			assert.Equal(t, "semantics/metrics/alpha.yaml", res.Accepted[0].Target)
			for _, n := range []string{"alpha", "beta"} {
				_, err := sem.Metric(ctx, n)
				require.NoError(t, err)
			}
			for _, id := range []string{a.ID, b.ID} {
				got, _ := props.Get(ctx, id)
				assert.Equal(t, StatusAccepted, got.Status)
			}
		})
	}
}

func TestAcceptMany_StopsAtStaleAndLeavesTheRest(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	a := fileMetric(t, props, "alpha", "sum(a)")
	b := fileMetric(t, props, "beta", "sum(b)")
	c := fileMetric(t, props, "gamma", "sum(c)")
	// beta already exists: the proposal to create it is stale.
	_, err := sem.PutSource(ctx, semantics.KindMetric, "beta", []byte("version: 1\nname: beta\nquery: q\nmeasure: sum(x)\n"))
	require.NoError(t, err)
	res := AcceptMany(ctx, props, sem, pinsOf(t, props, sem, a.ID, b.ID, c.ID))
	require.Error(t, res.Err)
	var stale *StaleError
	assert.ErrorAs(t, res.Err, &stale)
	assert.ErrorIs(t, res.Err, ErrConflict)
	assert.Equal(t, b.ID, res.FailedID)
	require.Len(t, res.Accepted, 1)
	assert.Equal(t, a.ID, res.Accepted[0].ID)

	src, err := sem.Source(ctx, semantics.KindMetric, "beta")
	require.NoError(t, err)
	assert.Contains(t, src.Text, "sum(x)", "a stale target is never overwritten")
	_, err = sem.Metric(ctx, "gamma")
	assert.ErrorIs(t, err, semantics.ErrNotFound, "nothing after the failure ran")
	got, _ := props.Get(ctx, c.ID)
	assert.Equal(t, StatusOpen, got.Status)
}

func TestAcceptMany_RefusesWhatChangedSinceItWasShown(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	a := fileMetric(t, props, "alpha", "sum(a)")
	b := fileMetric(t, props, "beta", "sum(b)")
	pins := pinsOf(t, props, sem, a.ID, b.ID)
	// beta's target is created after the person looked.
	_, err := sem.PutSource(ctx, semantics.KindMetric, "beta", []byte("version: 1\nname: beta\nquery: q\nmeasure: sum(x)\n"))
	require.NoError(t, err)
	res := AcceptMany(ctx, props, sem, pins)
	var again *ChangedAgainError
	require.ErrorAs(t, res.Err, &again)
	assert.Equal(t, b.ID, res.FailedID)
	assert.Len(t, res.Accepted, 1)
}

func TestAcceptMany_NeedsARealPin(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	a := fileMetric(t, props, "alpha", "sum(a)")
	res := AcceptMany(ctx, props, sem, []Pin{{ID: a.ID}})
	require.Error(t, res.Err)
	assert.ErrorIs(t, res.Err, ErrInvalid)
	assert.Empty(t, res.Accepted)
	_, err := sem.Metric(ctx, "alpha")
	assert.ErrorIs(t, err, semantics.ErrNotFound)
}

func TestAcceptMany_SecondProposalForTheSameTargetStopsAndDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	first := fileMetric(t, props, "alpha", "sum(a)")
	second := fileMetric(t, props, "alpha", "sum(a2)")
	res := AcceptMany(ctx, props, sem, pinsOf(t, props, sem, first.ID, second.ID))
	require.Error(t, res.Err)
	assert.ErrorIs(t, res.Err, ErrConflict)
	assert.Equal(t, second.ID, res.FailedID)
	require.Len(t, res.Accepted, 1)
	src, err := sem.Source(ctx, semantics.KindMetric, "alpha")
	require.NoError(t, err)
	assert.Contains(t, src.Text, "sum(a)")
	assert.NotContains(t, src.Text, "sum(a2)")
	got, _ := props.Get(ctx, second.ID)
	assert.Equal(t, StatusOpen, got.Status)
}

func TestAcceptMany_TwoGlossaryTermsBothAccept(t *testing.T) {
	ctx := context.Background()
	for _, c := range storeCases() {
		t.Run(c.name, func(t *testing.T) {
			props, sem := c.new(t)
			require.NoError(t, sem.PutGlossary(ctx, &semantics.Glossary{Text: "## A\n\nold\n"}))
			p1, p2 := fileTerm(t, props, "B"), fileTerm(t, props, "C")
			// shown together, before either is written
			res := AcceptMany(ctx, props, sem, pinsOf(t, props, sem, p1.ID, p2.ID))
			require.NoError(t, res.Err)
			require.Len(t, res.Accepted, 2)
			g, err := sem.Glossary(ctx)
			require.NoError(t, err)
			assert.Equal(t, []string{"A", "B", "C"}, g.Terms)
		})
	}
}

func TestAcceptMany_AlreadyDecidedStops(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	a := fileMetric(t, props, "alpha", "sum(a)")
	pins := pinsOf(t, props, sem, a.ID)
	require.NoError(t, Reject(ctx, props, a.ID))
	res := AcceptMany(ctx, props, sem, pins)
	assert.ErrorIs(t, res.Err, ErrNotOpen)
	assert.Equal(t, a.ID, res.FailedID)
}

func TestAcceptMany_EmptyListIsANoOp(t *testing.T) {
	res := AcceptMany(context.Background(), &Mock{}, &semantics.Mock{}, nil)
	require.NoError(t, res.Err)
	assert.Empty(t, res.FailedID)
	assert.NotNil(t, res.Accepted)
	assert.Empty(t, res.Accepted)
}

func TestAcceptMany_SecondProposalForTheSameGlossaryTermStops(t *testing.T) {
	ctx := context.Background()
	props, sem := &Mock{}, &semantics.Mock{}
	first := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": "Churn", "text": "one"}})
	second := mustBuild(t, Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": "Churn", "text": "two"}})
	require.NoError(t, props.Create(ctx, first))
	require.NoError(t, props.Create(ctx, second))
	res := AcceptMany(ctx, props, sem, pinsOf(t, props, sem, first.ID, second.ID))
	require.Error(t, res.Err)
	assert.ErrorIs(t, res.Err, ErrConflict)
	assert.Equal(t, second.ID, res.FailedID)
	require.Len(t, res.Accepted, 1)
	g, err := sem.Glossary(ctx)
	require.NoError(t, err)
	assert.Contains(t, g.Text, "one")
	assert.NotContains(t, g.Text, "two")
}

type failingDecide struct{ Store }

func (failingDecide) Decide(context.Context, string, string, string) error {
	return errors.New("disk full")
}

func TestAcceptMany_WrittenButNotMarkedIsReportedAsSuch(t *testing.T) {
	ctx := context.Background()
	mock, sem := &Mock{}, &semantics.Mock{}
	a := fileMetric(t, mock, "alpha", "sum(a)")
	pins := pinsOf(t, mock, sem, a.ID)
	res := AcceptMany(ctx, failingDecide{mock}, sem, pins)
	var nm *NotMarkedError
	require.ErrorAs(t, res.Err, &nm)
	assert.True(t, res.WrittenNotMarked)
	assert.Equal(t, a.ID, res.FailedID)
	assert.Empty(t, res.Accepted)
	_, err := sem.Metric(ctx, "alpha")
	require.NoError(t, err, "the file was written")
}
