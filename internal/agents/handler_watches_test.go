package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/watches"
)

// watchesFixture is the semantics fixture (a saved query net_revenue_base with a
// defaulted $min, plus helpers) with a saved query "stuck" and a metric.
func watchesFixture(t *testing.T, mode SandboxMode, opts ...fixtureOpt) *policyFixture {
	t.Helper()
	fx := semanticsFixture(t, mode, opts...)
	putSaved(t, fx, "stuck", "SELECT 1 AS id WHERE false")
	putSaved(t, fx, "needs_region", "SELECT * FROM sales WHERE region = $region",
		queries.Param{Name: "region", Type: "text", Required: true})
	putMetric(t, fx, "net_revenue", "net_revenue_base", "sum(amount)")
	return fx
}

func watchStore(fx *policyFixture) watches.Store {
	return watchProvider(fx.t, fx.h.access).Watches()
}

func stuckWatch(extra map[string]any) map[string]any {
	req := map[string]any{
		"name":     "stuck_orders",
		"question": map[string]any{"query": "stuck"},
		"when":     map[string]any{"rows": "any"},
	}
	for k, v := range extra {
		req[k] = v
	}
	return req
}

// canaryHistory is a history whose entries carry a reason, a value and a row
// count a tool must never repeat.
type canaryHistory struct{ entries []watches.Entry }

func (c canaryHistory) Entries(_ context.Context, watch string) ([]watches.Entry, error) {
	var out []watches.Entry
	for _, e := range c.entries {
		if watch == "" || e.Watch == watch {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestWatchTools_Registry(t *testing.T) {
	assert.Equal(t, "introspect", GetTool("list_watches").Category)
	assert.Equal(t, "introspect", GetTool("get_watch").Category)
	assert.Equal(t, "content", GetTool("write_watch").Category)
	assert.True(t, GetTool("write_watch").DryRunSafe)
	for _, name := range []string{"list_watches", "get_watch", "write_watch"} {
		assert.Empty(t, GetTool(name).Modes, "%s is offered in chat, MCP and task mode alike", name)
		assert.True(t, IsModeVisible(name, ModeMCP), name)
	}
}

// No tool checks a watch, reads a history reason or shows a result row: checking
// is the person's action, like assertions. Only the three watch tools mention
// watches at all, none offers to check or run one, and the method names that
// would are unknown.
func TestWatchTools_NothingChecksOrRunsAWatch(t *testing.T) {
	offers := regexp.MustCompile(`(?i)\b(check|run|execute|fire|trigger|evaluate|poll)\w*\b[^.]{0,40}\bwatch|watch\w*\b[^.]{0,20}\b(check|run|execute|fire|trigger|evaluate)\b`)
	negated := regexp.MustCompile(`(?i)(cannot|can't|never|nothing here)`)
	allowed := map[string]bool{"list_watches": true, "get_watch": true, "write_watch": true}
	for _, td := range ListTools() {
		assert.NotRegexp(t, `(?i)(check|run|exec|fire|trigger|history).*watch|watch.*(check|run|exec|fire|trigger|history)`, td.Name, "tool name offers to check a watch")
		blob := td.Description
		for _, p := range td.Params {
			assert.NotRegexp(t, `(?i)^(check|run|execute|history)$`, p.Name, "%s param", td.Name)
			blob += " " + p.Desc
		}
		if !allowed[td.Name] {
			assert.NotContains(t, strings.ToLower(blob), "watch", "%s mentions watches", td.Name)
			continue
		}
		for _, sentence := range strings.Split(blob, ". ") {
			if offers.MatchString(sentence) {
				assert.Regexp(t, negated, sentence, "%s offers to check or run a watch: %q", td.Name, sentence)
			}
		}
	}
	fx := watchesFixture(t, ModeFull)
	for _, m := range []string{"check_watch", "check_watches", "run_watch", "run_watches", "watch_history", "get_watch_history", "delete_watch", "fire_watch"} {
		resp := fx.call(m, map[string]any{"name": "x"})
		require.NotNil(t, resp.Error, m)
		assert.Equal(t, ErrMethodNotFound, resp.Error.Code, m)
	}
}

func TestListWatches_EmptyAndReadsInEveryTier(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		fx := watchesFixture(t, mode)
		resp := fx.call("list_watches", map[string]any{})
		require.Nil(t, resp.Error, "%s: %s", mode, errMsg(resp))
		assert.Empty(t, mapOf(t, resp.Result)["watches"], mode)
		resp = fx.call("get_watch", map[string]any{"name": "nope"})
		require.NotNil(t, resp.Error, mode)
		assert.Contains(t, resp.Error.Message, `there is no watch named "nope"`)
	}
}

func TestListAndGetWatch_FieldsAndCanonicalYAML(t *testing.T) {
	fx := watchesFixture(t, ModeReadOnly)
	st := watchStore(fx)
	above, err := watches.ParseThreshold("1000")
	require.NoError(t, err)
	require.NoError(t, st.Put(context.Background(), &watches.Watch{
		Name: "revenue_cap", Description: "Revenue over the cap",
		Question: watches.Question{Metric: "net_revenue"}, When: watches.Condition{Above: &above},
	}))
	require.NoError(t, st.Put(context.Background(), &watches.Watch{
		Name: "stuck_orders", Question: watches.Question{Query: "stuck", Params: map[string]string{"days": "1"}},
		When: watches.Condition{Rows: "any"},
	}))
	wm := watchMock(fx)
	wm.SetFile("bad_one", []byte("question: {query: x}\nwhen: {rows: sometimes}\nsurprise: 1\n"))

	res := mapOf(t, fx.call("list_watches", map[string]any{}).Result)
	rows := asRows(t, res["watches"])
	require.Len(t, rows, 3)
	by := map[string]map[string]any{}
	for _, r := range rows {
		by[asString(t, r["name"])] = r
	}
	rc := by["revenue_cap"]
	assert.Equal(t, "Revenue over the cap", rc["description"])
	assert.Equal(t, map[string]any{"metric": "net_revenue"}, rc["question"])
	assert.Equal(t, map[string]any{"above": "1000"}, rc["when"])
	assert.Equal(t, "above 1,000", rc["condition"])
	assert.NotEmpty(t, rc["rev"])
	assert.NotContains(t, rc, "last_checked", "never checked")
	assert.Equal(t, map[string]string{"days": "1"}, mapOf(t, by["stuck_orders"]["question"])["params"])
	assert.NotEmpty(t, by["bad_one"]["error"], "an unreadable file is listed with its error")
	assert.NotEmpty(t, by["bad_one"]["rev"])

	got := mapOf(t, fx.call("get_watch", map[string]any{"name": "revenue_cap"}).Result)
	assert.Equal(t, rc["rev"], got["rev"])
	assert.Equal(t, "description: Revenue over the cap\nquestion:\n  metric: net_revenue\nwhen:\n  above: 1000\n", got["yaml"])

	resp := fx.call("get_watch", map[string]any{"name": "bad_one"})
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "cannot be read")
}

// list_watches reports when a watch was last checked and the status, and
// nothing else the history holds.
func TestListWatches_StatusOnlyNeverAReasonOrAValue(t *testing.T) {
	fx := watchesFixture(t, ModeReadOnly)
	require.Nil(t, fx.call("list_watches", nil).Error)
	require.NoError(t, watchStore(fx).Put(context.Background(), &watches.Watch{
		Name: "stuck_orders", Question: watches.Question{Query: "stuck"}, When: watches.Condition{Rows: "any"},
	}))
	rows := 3
	older := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	hist := canaryHistory{entries: []watches.Entry{
		{Watch: "stuck_orders", At: older, Status: watches.StatusAlert, Rows: &rows, Reason: "CANARY-REASON stuck_orders returned 3 rows for alice@example.com", Value: "424242"},
		{Watch: "stuck_orders", At: newer, Status: watches.StatusError, Reason: "could not run: Conversion Error CANARY-ERROR", DurationMS: 77},
		{Watch: "someone_else", At: newer, Status: watches.StatusOK, Reason: "CANARY-OTHER"},
	}}
	setHistory(t, fx, hist)

	resp := fx.call("list_watches", map[string]any{})
	require.Nil(t, resp.Error)
	row := asRows(t, mapOf(t, resp.Result)["watches"])[0]
	assert.Equal(t, "2026-11-02T09:30:00Z", row["last_checked"])
	assert.Equal(t, "error", row["last_status"])

	for _, method := range []string{"list_watches", "get_watch"} {
		resp := fx.call(method, map[string]any{"name": "stuck_orders"})
		require.Nil(t, resp.Error, method)
		raw, err := json.Marshal(resp.Result)
		require.NoError(t, err)
		for _, leak := range []string{"CANARY", "alice@example.com", "424242", "reason", "duration", "sample", "someone_else"} {
			assert.NotContains(t, string(raw), leak, "%s must not show %q", method, leak)
		}
	}

	// A history that cannot be read does not hide the watches.
	setHistory(t, fx, failingHistory{})
	resp = fx.call("list_watches", map[string]any{})
	require.Nil(t, resp.Error)
	row = asRows(t, mapOf(t, resp.Result)["watches"])[0]
	assert.NotContains(t, row, "last_checked")
}

type failingHistory struct{}

func (failingHistory) Entries(context.Context, string) ([]watches.Entry, error) {
	return nil, errors.New("history is unreadable: /home/zac/secret/path")
}

func TestWriteWatch_CreateThenUpdateWithRev(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	resp := fx.call("write_watch", stuckWatch(map[string]any{"description": "Orders stuck in pending"}))
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, true, res["saved"])
	assert.Equal(t, true, res["created"])
	assert.Equal(t, "description: Orders stuck in pending\nquestion:\n  query: stuck\nwhen:\n  rows: any\n", res["file"])
	assert.Contains(t, res["note"], "not checked")
	assert.Contains(t, res["note"], "slaw watch check")
	rev := asString(t, res["rev"])
	require.NotEmpty(t, rev)

	got, err := watchStore(fx).Get(context.Background(), "stuck_orders")
	require.NoError(t, err)
	assert.Equal(t, rev, got.Rev)
	assert.Equal(t, "any", got.When.Rows)

	// update with the rev
	resp = fx.call("write_watch", stuckWatch(map[string]any{"rev": rev, "when": map[string]any{"rows": "none"}}))
	require.Nil(t, resp.Error, errMsg(resp))
	res = mapOf(t, resp.Result)
	assert.Equal(t, false, res["created"])
	assert.NotEqual(t, rev, res["rev"])
	got, _ = watchStore(fx).Get(context.Background(), "stuck_orders")
	assert.Equal(t, "none", got.When.Rows)
	assert.Empty(t, got.Description, "an update replaces the file")

	// the stale rev is a conflict and changes nothing
	resp = fx.call("write_watch", stuckWatch(map[string]any{"rev": rev, "when": map[string]any{"rows": "any"}}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "changed since the rev you sent")
	got, _ = watchStore(fx).Get(context.Background(), "stuck_orders")
	assert.Equal(t, "none", got.When.Rows)
}

func TestWriteWatch_ExistsWithoutRevAndRevWithoutFile(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	require.Nil(t, fx.call("write_watch", stuckWatch(nil)).Error)
	before, _ := watchStore(fx).Get(context.Background(), "stuck_orders")

	resp := fx.call("write_watch", stuckWatch(map[string]any{"when": map[string]any{"rows": "none"}}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already exists")
	assert.Contains(t, resp.Error.Message, "rev")
	data := mapOf(t, resp.Error.Data)
	assert.Contains(t, data["file"], "rows: none", "the canonical text of the refused request comes back")
	after, _ := watchStore(fx).Get(context.Background(), "stuck_orders")
	assert.Equal(t, before.Rev, after.Rev)

	resp = fx.call("write_watch", stuckWatch(map[string]any{"name": "nothing_here", "rev": "abc"}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "no watch named")
}

func TestWriteWatch_ReplacesAnUnreadableFileWithItsRev(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	watchMock(fx).SetFile("stuck_orders", []byte("not: [valid"))
	row := asRows(t, mapOf(t, fx.call("list_watches", nil).Result)["watches"])[0]
	require.NotEmpty(t, row["error"])

	resp := fx.call("write_watch", stuckWatch(nil))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "already exists")

	resp = fx.call("write_watch", stuckWatch(map[string]any{"rev": row["rev"]}))
	require.Nil(t, resp.Error, errMsg(resp))
	_, err := watchStore(fx).Get(context.Background(), "stuck_orders")
	require.NoError(t, err)
}

func TestWriteWatch_InvalidShapesAreRefusedWithTheDecodersWords(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	cases := []struct {
		name   string
		mut    map[string]any
		expect string
	}{
		{"bad name", map[string]any{"name": "Bad Name"}, "watch name"},
		{"reserved name", map[string]any{"name": "nul"}, "reserved"},
		{"no condition", map[string]any{"when": map[string]any{}}, "when"},
		{"two conditions", map[string]any{"when": map[string]any{"rows": "any", "above": 1}}, "exactly one"},
		{"bad rows", map[string]any{"when": map[string]any{"rows": "sometimes"}}, "rows"},
		{"exponent threshold", map[string]any{"when": map[string]any{"above": "1e3"}}, "plain decimal"},
		{"signed threshold", map[string]any{"when": map[string]any{"below": "+5"}}, "plain decimal"},
		{"outside needs two", map[string]any{"when": map[string]any{"outside": []any{1}}}, "two numbers"},
		{"outside order", map[string]any{"when": map[string]any{"outside": []any{9, 1}}}, "outside"},
		{"unknown when field", map[string]any{"when": map[string]any{"between": 1}}, "unknown field"},
		{"both query and metric", map[string]any{"question": map[string]any{"query": "stuck", "metric": "net_revenue"}}, "exactly one"},
		{"neither", map[string]any{"question": map[string]any{}}, "exactly one"},
		{"metric with params", map[string]any{"question": map[string]any{"metric": "net_revenue", "params": map[string]any{"a": 1}}}, "params"},
		{"unknown question field", map[string]any{"question": map[string]any{"sql": "SELECT 1"}}, "unknown field"},
		{"question not an object", map[string]any{"question": "stuck"}, "must be an object"},
		{"description over a line", map[string]any{"description": "two\nlines"}, "description"},
		{"description too long", map[string]any{"description": strings.Repeat("x", 201)}, "description"},
		{"non-string rev", map[string]any{"rev": 7}, "rev must be a string"},
		{"param object value", map[string]any{"question": map[string]any{"query": "stuck", "params": map[string]any{"a": map[string]any{}}}}, "params.a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := fx.call("write_watch", stuckWatch(c.mut))
			require.NotNil(t, resp.Error)
			assert.Equal(t, ErrInvalidParams, resp.Error.Code)
			assert.Contains(t, resp.Error.Message, c.expect)
		})
	}
	list, err := watchStore(fx).List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, list, "nothing was written")
}

func TestWriteWatch_NumbersAreTheTextOfTheFile(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	resp := fx.call("write_watch", map[string]any{
		"name": "band", "question": map[string]any{"metric": "net_revenue"},
		"when": map[string]any{"outside": []any{float64(10), "99.5"}},
	})
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Contains(t, mapOf(t, resp.Result)["file"], "outside: [10, 99.5]")

	resp = fx.call("write_watch", map[string]any{
		"name": "cap", "question": map[string]any{"query": "net_revenue_base", "params": map[string]any{"min": float64(5)}},
		"when": map[string]any{"above": float64(0.95)},
	})
	require.Nil(t, resp.Error, errMsg(resp))
	file := asString(t, mapOf(t, resp.Result)["file"])
	assert.Contains(t, file, "above: 0.95")
	assert.Contains(t, file, `min: "5"`)
}

func TestWriteWatch_QuestionMustExist(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	resp := fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"query": "no_such_query"}}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, `saved query "no_such_query" does not exist`)
	assert.Contains(t, resp.Error.Message, "save_query")
	assert.Contains(t, mapOf(t, resp.Error.Data)["file"], "query: no_such_query", "the canonical text comes back")

	resp = fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"metric": "no_such_metric"}}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, `no metric named "no_such_metric"`)
	assert.Contains(t, resp.Error.Message, "propose_definition")

	resp = fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"query": "stuck", "params": map[string]any{"nope": 1}}}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "no parameter named")

	list, _ := watchStore(fx).List(context.Background())
	assert.Empty(t, list)
}

func TestWriteWatch_RequiredParameterWithoutAValueWarnsButSaves(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite)
	resp := fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"query": "needs_region"}}))
	require.Nil(t, resp.Error, errMsg(resp))
	res := mapOf(t, resp.Result)
	assert.Equal(t, true, res["saved"])
	warnings := asStrings(t, res["warnings"])
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], `"region"`)
	assert.Contains(t, warnings[0], "question.params.region")

	// given a value there is no warning
	resp = fx.call("write_watch", stuckWatch(map[string]any{"name": "with_region",
		"question": map[string]any{"query": "needs_region", "params": map[string]any{"region": "east"}}}))
	require.Nil(t, resp.Error, errMsg(resp))
	assert.NotContains(t, mapOf(t, resp.Result), "warnings")
}

func TestWriteWatch_Tiers(t *testing.T) {
	for _, mode := range []SandboxMode{ModeReadOnly, ModeReadWrite, ModeFull} {
		fx := watchesFixture(t, mode)
		resp := fx.call("write_watch", stuckWatch(nil))
		list, _ := watchStore(fx).List(context.Background())
		if mode == ModeReadOnly {
			require.NotNil(t, resp.Error)
			assert.Equal(t, ErrPermission, resp.Error.Code)
			assert.Contains(t, resp.Error.Message, "not allowed in read-only mode")
			assert.Empty(t, list, "read-only writes nothing")
			continue
		}
		require.Nil(t, resp.Error, "%s: %s", mode, errMsg(resp))
		assert.Len(t, list, 1, mode)
	}
}

// Headless with "Ask before changes" on: an invalid request gets its own error,
// a valid one is told it is valid and ready (with the file and warnings it would
// write) and only needs the person's OK. Nothing is written either way.
func TestWriteWatch_HeadlessWithAskIsValidatedFirst(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite, withAsk(true))

	resp := fx.call("write_watch", stuckWatch(map[string]any{"when": map[string]any{"above": "1e3"}}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "plain decimal")

	resp = fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"query": "no_such_query"}}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrInvalidParams, resp.Error.Code, "a missing saved query is a validation error, not an approval problem")

	resp = fx.call("write_watch", stuckWatch(map[string]any{"question": map[string]any{"query": "needs_region"}}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "valid and ready")
	assert.Contains(t, resp.Error.Message, "NOT saved")
	assert.Contains(t, resp.Error.Message, "watches/stuck_orders.yaml")
	data := mapOf(t, resp.Error.Data)
	assert.Equal(t, true, data["valid"])
	assert.Equal(t, true, data["needs_approval"])
	assert.Equal(t, true, data["nothing_written"])
	assert.Contains(t, data["file"], "query: needs_region")
	assert.NotEmpty(t, data["warnings"])

	list, _ := watchStore(fx).List(context.Background())
	assert.Empty(t, list)
}

func TestWriteWatch_ApprovalCardAndDecision(t *testing.T) {
	fx := watchesFixture(t, ModeReadWrite, withAsk(true), withApprover())
	resp := fx.call("write_watch", stuckWatch(nil))
	require.Nil(t, resp.Error, errMsg(resp))
	require.Equal(t, 1, fx.approver.count())
	card := fx.approver.asked[0]
	assert.Equal(t, "Save watch stuck_orders: query stuck, fires when any rows", card.Summary)
	assert.Equal(t, "write_watch", card.Tool)
	list, _ := watchStore(fx).List(context.Background())
	assert.Len(t, list, 1)

	rev := list[0].Rev
	resp = fx.call("write_watch", stuckWatch(map[string]any{"rev": rev, "when": map[string]any{"rows": "none"}}))
	require.Nil(t, resp.Error, errMsg(resp))
	assert.Equal(t, "Replace watch stuck_orders: query stuck, fires when no rows", fx.approver.asked[1].Summary)

	fx.approver.decision = ErrApprovalDeclined
	resp = fx.call("write_watch", stuckWatch(map[string]any{"name": "other", "when": map[string]any{"rows": "any"}}))
	require.NotNil(t, resp.Error)
	assert.Equal(t, ErrApprovalNeeded, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "declined")
	_, err := watchStore(fx).Get(context.Background(), "other")
	assert.ErrorIs(t, err, watches.ErrNotFound)

	// With someone to ask (as for every content tool) the card is shown first and
	// an invalid request is refused after the OK; nothing is written either way.
	fx.approver.decision = nil
	resp = fx.call("write_watch", stuckWatch(map[string]any{"name": "Bad Name"}))
	require.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "not valid")
	_, err = watchStore(fx).Get(context.Background(), "Bad Name")
	assert.Error(t, err)
}

func TestWriteWatch_NeverTouchesTheHistory(t *testing.T) {
	fx := watchesFixture(t, ModeFull)
	hist := &countingHistory{}
	setHistory(t, fx, hist)
	require.Nil(t, fx.call("write_watch", stuckWatch(nil)).Error)
	_, ok := any(watchProvider(t, fx.h.access).WatchHistory()).(interface {
		Append(context.Context, watches.Entry) error
	})
	assert.False(t, ok, "the agent's history has no Append")
	assert.Zero(t, hist.reads, "writing a watch does not read the history either")
}

type countingHistory struct{ reads int }

func (c *countingHistory) Entries(context.Context, string) ([]watches.Entry, error) {
	c.reads++
	return nil, nil
}

func TestWatchTools_NotAvailableWithoutAWatchesFolder(t *testing.T) {
	h := NewHandler(&bareAccess{mockAccess: &mockAccess{}}, &mockEngine{}, &SandboxConfig{Mode: ModeFull, MaxQueryRows: 10}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, m := range []string{"list_watches", "get_watch", "write_watch"} {
		resp := h.Handle(context.Background(), &Request{JSONRPC: "2.0", ID: 1, Method: m, Params: map[string]any{"name": "x", "question": map[string]any{"query": "q"}, "when": map[string]any{"rows": "any"}}})
		require.NotNil(t, resp.Error, m)
		assert.Contains(t, resp.Error.Message, "watches are not available", m)
	}
}

// bareAccess hides the mock's watches, as an access double of another package
// that predates watches would.
type bareAccess struct{ *mockAccess }

func (bareAccess) Watches()      {}
func (bareAccess) WatchHistory() {}

func watchProvider(t testing.TB, a WorkspaceAccess) WatchesProvider {
	t.Helper()
	p, ok := a.(WatchesProvider)
	require.True(t, ok, "the access double has watches")
	return p
}

func watchMock(fx *policyFixture) *watches.Mock {
	m, ok := watchStore(fx).(*watches.Mock)
	require.True(fx.t, ok)
	return m
}

func setHistory(t testing.TB, fx *policyFixture, h watches.HistoryReader) {
	t.Helper()
	acc, ok := fx.h.access.(*policyAccess)
	require.True(t, ok)
	acc.mockAccess.mu.Lock()
	defer acc.mockAccess.mu.Unlock()
	acc.mockAccess.whist = h
}

func asStrings(t testing.TB, v any) []string {
	t.Helper()
	out, ok := v.([]string)
	require.True(t, ok, "expected a list of strings, got %T", v)
	return out
}
