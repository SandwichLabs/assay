package agents

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/watches"
)

// A JSON number is a float64 by the time it arrives, so one with more than 15
// significant digits has already been rounded: it is refused, and a string
// keeps it exact.
func TestWriteWatch_RoundedNumbersAreRefusedNotSilentlyChanged(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	for name, mut := range map[string]map[string]any{
		"above":   {"when": map[string]any{"above": float64(123456789012345678901)}},
		"below":   {"when": map[string]any{"below": float64(9007199254740993)}},
		"outside": {"when": map[string]any{"outside": []any{float64(1), float64(9007199254740993)}}},
		"param": {"question": map[string]any{"query": "net_revenue_base", "params": map[string]any{"min": float64(9007199254740993)}},
			"when": map[string]any{"rows": "any"}},
	} {
		resp := fx.call("write_watch", stuckWatch(mut))
		require.NotNil(t, resp.Error, name)
		assert.Contains(t, resp.Error.Message, "send it as a string to keep it exact", name)
		assert.Contains(t, resp.Error.Message, "15 significant digits", name)
	}
	list, _ := watchStore(fx).List(context.Background())
	assert.Empty(t, list)

	// Exact as a string, and short floats still pass.
	resp := fx.call("write_watch", stuckWatch(map[string]any{"when": map[string]any{"above": "123456789012345678901"}}))
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Contains(t, asString(t, mapOf(t, resp.Result)["file"]), "above: 123456789012345678901")
	for _, ok := range []float64{1000, 0.95, 999999999999999, -12.5, 1e21} {
		_, err := numberText(ok)
		assert.NoError(t, err, ok)
	}
	_, err := numberText(float64(1234567890123456))
	assert.Error(t, err, "16 digits")
	got, err := numberText(int64(9007199254740993))
	require.NoError(t, err)
	assert.Equal(t, "9007199254740993", got)
}

func TestWriteWatch_CaseVariantAndCreateRaceHaveTheirOwnMessages(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	watchMock(fx).SetFile("Stuck_Orders", []byte("question:\n  query: stuck\nwhen:\n  rows: any\n"))
	resp := fx.call("write_watch", stuckWatch(nil))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "different capitalisation")
	assert.NotContains(t, resp.Error.Message, "changed since the rev you sent")

	acc, ok := fx.h.access.(*policyAccess)
	require.True(t, ok)
	fx.h.access = &raceAccess{policyAccess: acc}
	resp = fx.call("write_watch", stuckWatch(map[string]any{"name": "racy"}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "was created while this was being checked")
	assert.NotContains(t, resp.Error.Message, "changed since the rev you sent")
}

// raceAccess reports no file at Source and refuses the create, as a file that
// appears between the check and the write does.
type raceAccess struct{ *policyAccess }

func (r *raceAccess) Watches() watches.WriteStore { return raceStore{r.policyAccess.Watches()} }

type raceStore struct{ watches.WriteStore }

func (raceStore) Source(context.Context, string) (*watches.Source, error) {
	return nil, watches.ErrNotFound
}

func (raceStore) Put(context.Context, *watches.Watch, ...watches.WriteOption) error {
	return watches.ErrConflict
}

func TestGetWatch_BadNameStatesTheNameRule(t *testing.T) {
	fx := watchesFixture(t, ModeReadOnly)
	resp := fx.call("get_watch", map[string]any{"name": "Stuck Orders"})
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "lower-case letters, digits and underscores")
	assert.NotContains(t, resp.Error.Message, "rev you")
}

func TestWriteWatch_ApprovalCardShowsTheCanonicalFile(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite, withAsk(true), withApprover())
	resp := fx.call("write_watch", stuckWatch(map[string]any{"description": "Pending orders",
		"question": map[string]any{"query": "net_revenue_base", "params": map[string]any{"min": 100}}}))
	require.Nil(t, resp.Error, errMsg(resp))
	card := fx.approver.asked[0]
	assert.Equal(t, "description: Pending orders\nquestion:\n  query: net_revenue_base\n  params:\n    min: \"100\"\nwhen:\n  rows: any\n", card.SQL)
}

func TestWriteWatch_MetricWithAMissingQueryWarnsTheMetricIsThePersons(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	putMetric(t, fx, "orphan", "gone", "sum(amount)")
	resp := fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"metric": "orphan"}}))
	require.Nil(t, resp.Error, errMsg(resp))
	warnings := asStrings(t, mapOf(t, resp.Result)["warnings"])
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "the person fixes the metric's saved query")
}

// The agent's side stays narrow and never reaches into the workspace package.
func TestAgentsDoesNotDependOnWorkspace(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	require.NoError(t, err, string(out))
	for _, dep := range strings.Fields(string(out)) {
		assert.False(t, strings.HasSuffix(dep, "/internal/workspace") || strings.Contains(dep, "/internal/workspace/"), "internal/agents imports %s", dep)
	}
}

func methodNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumMethod())
	for i := 0; i < t.NumMethod(); i++ {
		names = append(names, t.Method(i).Name)
	}
	return names
}

// Pins what the agent can do with watches: a store with no Delete or PutSource,
// a history with no Append.
func TestWatchAccessMethodSets(t *testing.T) {
	assert.Equal(t, []string{"WatchHistory", "Watches"}, methodNames(reflect.TypeOf((*WatchesProvider)(nil)).Elem()))
	assert.Equal(t, []string{"Get", "List", "Put", "Source"}, methodNames(reflect.TypeOf((*watches.WriteStore)(nil)).Elem()))
	assert.Equal(t, []string{"Entries"}, methodNames(reflect.TypeOf((*watches.HistoryReader)(nil)).Elem()))
}
