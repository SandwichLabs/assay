package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
	"github.com/SandwichLabs/slaw/internal/watches"
)

// --- watches: list_watches, get_watch, write_watch (E7 W3) -------------------
//
// A watch is a standing question with a condition, a file under watches/
// (docs/reference/watches.md). The agent may read the files, see when each was
// last checked and with what status, and write a file. It can never check one:
// no tool runs a watch, reads a history reason or sees a result row. Checking
// is the person's action (`slaw watch check`, Studio's Check), like the
// semantic layer's assertions. list_watches is handed a watches.HistoryReader
// (no Append in its type) and reports a status and a time only.

// WatchesProvider is implemented by a WorkspaceAccess that has a workspace with
// a watches/ folder. Like ArtifactsProvider it is optional, so the access
// doubles of other packages need not change; without it the tools say so.
type WatchesProvider interface {
	Watches() watches.Store
	WatchHistory() watches.HistoryReader
}

const errWatchesUnavailable = "watches are not available in this workspace"

func (h *Handler) watchesAccess() (WatchesProvider, error) {
	if h.access != nil {
		if p, ok := h.access.(WatchesProvider); ok && p.Watches() != nil {
			return p, nil
		}
	}
	return nil, errors.New(errWatchesUnavailable)
}

// checkNote is said in every write result: the agent cannot check a watch.
const watchCheckNote = "Written, not checked. Nothing here can check a watch: the person checks it in Slaw's Watches view or with `slaw watch check`."

// whenFields is a condition as the tools show it: thresholds as the text of the
// file, so what is shown is what is compared.
func whenFields(c watches.Condition) map[string]any {
	out := map[string]any{}
	if c.Rows != "" {
		out["rows"] = c.Rows
	}
	if c.Above != nil {
		out["above"] = c.Above.String()
	}
	if c.Below != nil {
		out["below"] = c.Below.String()
	}
	if len(c.Outside) > 0 {
		pair := make([]string, len(c.Outside))
		for i, t := range c.Outside {
			pair[i] = t.String()
		}
		out["outside"] = pair
	}
	return out
}

func questionFields(q watches.Question) map[string]any {
	out := map[string]any{}
	if q.Query != "" {
		out["query"] = q.Query
	}
	if len(q.Params) > 0 {
		out["params"] = q.Params
	}
	if q.Metric != "" {
		out["metric"] = q.Metric
	}
	return out
}

// watchFields starts a row: name, rev and the error of an unreadable file.
func watchFields(w *watches.Watch) map[string]any {
	row := map[string]any{"name": w.Name}
	if w.Rev != "" {
		row["rev"] = w.Rev
	}
	if w.Error != "" {
		row["error"] = scrubStoreText(w.Error)
		return row
	}
	if w.Description != "" {
		row["description"] = scrubStoreText(w.Description)
	}
	row["question"] = questionFields(w.Question)
	row["when"] = whenFields(w.When)
	row["condition"] = w.When.Describe()
	return row
}

func (h *Handler) handleListWatches(ctx context.Context, req *Request) *Response {
	acc, err := h.watchesAccess()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	list, err := acc.Watches().List(ctx)
	if err != nil {
		h.logger.Warn("watch store error", "action", "list", "error", err)
		return NewErrorResponse(req.ID, ErrInternal, "failed to list watches: "+scrubStoreText(watches.ReadableError(err)))
	}
	// The history gives a time and a status per watch. A history that cannot be
	// read leaves them out; the watches still list.
	var sum map[string]watches.Summary
	if entries, herr := acc.WatchHistory().Entries(ctx, ""); herr == nil {
		sum = watches.Summarize(entries)
	}
	rows := make([]map[string]any, 0, len(list))
	for i := range list {
		row := watchFields(&list[i])
		if s, ok := sum[list[i].Name]; ok && s.LastChecked != nil {
			row["last_checked"] = s.LastChecked.UTC().Format(time.RFC3339)
			row["last_status"] = string(s.LastStatus)
		}
		rows = append(rows, row)
	}
	return NewResponse(req.ID, map[string]any{"watches": rows})
}

func (h *Handler) handleGetWatch(ctx context.Context, req *Request) *Response {
	acc, err := h.watchesAccess()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	name := strings.TrimSpace(stringParam(req.Params["name"]))
	w, err := acc.Watches().Get(ctx, name)
	switch {
	case errors.Is(err, watches.ErrNotFound):
		return NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("there is no watch named %q (see list_watches)", truncateRunesText(name, 80)))
	case errors.Is(err, watches.ErrInvalid):
		return NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText(fmt.Sprintf("the watch %q cannot be read: %s. list_watches has its rev; send it to write_watch to replace the file.", name, watches.ReadableError(err))))
	case err != nil:
		h.logger.Warn("watch store error", "action", "get", "error", err)
		return NewErrorResponse(req.ID, ErrInternal, "failed to read the watch: "+scrubStoreText(watches.ReadableError(err)))
	}
	data, err := watches.Encode(w)
	if err != nil {
		return NewErrorResponse(req.ID, ErrInternal, "failed to encode the watch: "+scrubStoreText(watches.ReadableError(err)))
	}
	res := watchFields(w)
	res["yaml"] = string(data)
	return NewResponse(req.ID, res)
}

// --- write_watch --------------------------------------------------------------

func (h *Handler) handleWriteWatch(ctx context.Context, req *Request) *Response {
	acc, err := h.watchesAccess()
	if err != nil {
		return NewErrorResponse(req.ID, ErrInvalidParams, err.Error())
	}
	w, rev, errResp := watchFromRequest(req)
	if errResp != nil {
		return errResp
	}
	canonical, errResp := canonicalWatch(req, w)
	if errResp != nil {
		return errResp
	}
	st := acc.Watches()
	if errResp := checkWatchTarget(ctx, req, st, w.Name, rev, canonical); errResp != nil {
		return errResp
	}
	warnings, errResp := h.checkWatchQuestion(ctx, req, w, canonical)
	if errResp != nil {
		return errResp
	}
	extra := map[string]any{"file": canonical}
	if len(warnings) > 0 {
		extra["warnings"] = warnings
	}
	if isDryRun(ctx) {
		return dryRunOKWith(req.ID, extra)
	}
	w.Rev = rev
	if err := st.Put(ctx, w, watches.WithRev(rev)); err != nil {
		switch {
		case errors.Is(err, watches.ErrConflict):
			return NewErrorResponse(req.ID, ErrInvalidParams, staleWatchMessage(w.Name))
		case errors.Is(err, watches.ErrInvalid):
			return withFile(NewErrorResponse(req.ID, ErrInvalidParams, scrubFeedbackText("not saved: "+watches.ReadableError(err))), canonical)
		}
		h.logger.Warn("watch store error", "action", "write", "error", err)
		return NewErrorResponse(req.ID, ErrInternal, "failed to save the watch: "+scrubStoreText(watches.ReadableError(err)))
	}
	h.logger.Info("agent saved watch", "name", w.Name, "created", rev == "")
	res := map[string]any{"name": w.Name, "saved": true, "created": rev == "", "rev": w.Rev, "file": canonical, "note": watchCheckNote}
	if len(warnings) > 0 {
		res["warnings"] = warnings
	}
	return NewResponse(req.ID, res)
}

// withFile hands the canonical text of a rejected request back to the agent
// when there is one, so it can see what the file would have been.
func withFile(resp *Response, canonical string) *Response {
	if canonical != "" && resp.Error != nil {
		resp.Error.Data = map[string]any{"file": canonical}
	}
	return resp
}

func staleWatchMessage(name string) string {
	return fmt.Sprintf("the watch %q changed since the rev you sent (a person or another tool edited it), so nothing was saved. Call get_watch again, merge your change into what it returns, and send the new rev.", name)
}

// watchFromRequest reads write_watch's arguments into a Watch (not yet valid:
// the decoder's rules run in canonicalWatch) and the rev to replace.
func watchFromRequest(req *Request) (*watches.Watch, string, *Response) {
	bad := func(format string, args ...any) (*watches.Watch, string, *Response) {
		return nil, "", NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf(format, args...))
	}
	for _, f := range []string{"name", "description", "rev"} {
		if v, present := req.Params[f]; present && v != nil {
			if _, ok := v.(string); !ok {
				return bad("%s must be a string", f)
			}
		}
	}
	q, err := questionFromParams(req.Params["question"])
	if err != nil {
		return bad("question: %s", err)
	}
	c, err := conditionFromParams(req.Params["when"])
	if err != nil {
		return bad("when: %s", err)
	}
	w := &watches.Watch{
		Name:        strings.TrimSpace(stringParam(req.Params["name"])),
		Description: strings.TrimSpace(stringParam(req.Params["description"])),
		Question:    q,
		When:        c,
	}
	return w, strings.TrimSpace(stringParam(req.Params["rev"])), nil
}

func questionFromParams(raw any) (watches.Question, error) {
	var q watches.Question
	m, ok := raw.(map[string]any)
	if !ok {
		return q, errors.New("must be an object: {query, params?} or {metric}")
	}
	for k := range m {
		if k != "query" && k != "params" && k != "metric" {
			return q, fmt.Errorf("unknown field %q (use query and params, or metric)", truncateRunesText(k, 40))
		}
	}
	for _, f := range []string{"query", "metric"} {
		if v, present := m[f]; present && v != nil {
			if _, ok := v.(string); !ok {
				return q, fmt.Errorf("%s must be a string", f)
			}
		}
	}
	q.Query = strings.TrimSpace(stringParam(m["query"]))
	q.Metric = strings.TrimSpace(stringParam(m["metric"]))
	if raw, present := m["params"]; present && raw != nil {
		pm, ok := raw.(map[string]any)
		if !ok {
			return q, errors.New("params must be an object {name: value}")
		}
		q.Params = map[string]string{}
		for k, v := range pm {
			s, ok := paramText(v)
			if !ok {
				return q, fmt.Errorf("params.%s must be a string, number or boolean", truncateRunesText(k, 40))
			}
			q.Params[k] = s
		}
	}
	return q, nil
}

// paramText is a parameter value as the text a person would give to
// `slaw query --saved -p name=value`.
func paramText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	}
	return numberText(v)
}

// numberText is a JSON number as plain decimal text. A float64 argument cannot
// carry more than 15 significant digits, so an exact threshold is sent as a
// string.
func numberText(v any) (string, bool) {
	switch x := v.(type) {
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case json.Number:
		return x.String(), true
	}
	return "", false
}

func conditionFromParams(raw any) (watches.Condition, error) {
	var c watches.Condition
	m, ok := raw.(map[string]any)
	if !ok {
		return c, errors.New("must be an object with exactly one of rows (any|none), above, below or outside")
	}
	for k := range m {
		switch k {
		case "rows", "above", "below", "outside":
		default:
			return c, fmt.Errorf("unknown field %q (use rows, above, below or outside)", truncateRunesText(k, 40))
		}
	}
	if v, present := m["rows"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			return c, errors.New("rows must be \"any\" or \"none\"")
		}
		c.Rows = strings.TrimSpace(s)
	}
	var err error
	if c.Above, err = thresholdFromParam("above", m["above"]); err != nil {
		return c, err
	}
	if c.Below, err = thresholdFromParam("below", m["below"]); err != nil {
		return c, err
	}
	if v, present := m["outside"]; present && v != nil {
		list, ok := v.([]any)
		if !ok || len(list) != 2 {
			return c, errors.New("outside must be a list of two numbers [low, high]")
		}
		for i, e := range list {
			t, err := thresholdFromParam([]string{"outside (low)", "outside (high)"}[i], e)
			if err != nil {
				return c, err
			}
			c.Outside = append(c.Outside, *t)
		}
	}
	return c, nil
}

// thresholdFromParam reads a threshold given as a JSON number or as text, with
// the model's own strict parser: a quoted "100" is fine here (it is the text of
// the file), an exponent or a sign is not.
func thresholdFromParam(label string, v any) (*watches.Threshold, error) {
	if v == nil {
		return nil, nil
	}
	text, ok := v.(string)
	if !ok {
		if text, ok = numberText(v); !ok {
			return nil, fmt.Errorf("%s must be a number", label)
		}
	}
	t, err := watches.ParseThreshold(strings.TrimSpace(text))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return &t, nil
}

// canonicalWatch runs the decoder's rules, the way Studio's PreviewWatch does:
// Encode validates and renders the file, and the strict decoder must read that
// text back. A refusal carries the decoder's own message.
func canonicalWatch(req *Request, w *watches.Watch) (string, *Response) {
	data, err := watches.Encode(w)
	if err == nil {
		_, err = watches.Decode(w.Name, data)
	}
	if err != nil {
		return "", NewErrorResponse(req.ID, ErrInvalidParams, scrubFeedbackText("not saved, because the watch is not valid: "+watches.ReadableError(err)))
	}
	return string(data), nil
}

// checkWatchTarget settles whether a write creates or replaces: a create never
// replaces a file, and a replace needs the rev the agent read.
func checkWatchTarget(ctx context.Context, req *Request, st watches.Store, name, rev, canonical string) *Response {
	fail := func(format string, args ...any) *Response {
		return withFile(NewErrorResponse(req.ID, ErrInvalidParams, "not saved: "+fmt.Sprintf(format, args...)), canonical)
	}
	src, err := st.Source(ctx, name)
	switch {
	case errors.Is(err, watches.ErrNotFound):
		if rev != "" {
			return fail("there is no watch named %q to replace (see list_watches). Omit rev to create a new one.", name)
		}
		return nil
	case err != nil:
		return NewErrorResponse(req.ID, ErrInternal, "failed to read the current watch: "+scrubStoreText(watches.ReadableError(err)))
	case rev == "":
		return fail("a watch named %q already exists, so this would replace it. Read it with get_watch (list_watches shows an unreadable one) and send its rev, or pick another name.", name)
	case rev != src.Rev:
		return fail("%s", staleWatchMessage(name))
	}
	return nil
}

// checkWatchQuestion refuses a watch over a saved query or metric that does not
// exist (the file would be valid and every check would fail), and warns about
// what it can see would fail a check later: a required parameter with no value.
func (h *Handler) checkWatchQuestion(ctx context.Context, req *Request, w *watches.Watch, canonical string) ([]string, *Response) {
	if w.Question.Metric != "" {
		return h.checkWatchMetric(ctx, req, w, canonical)
	}
	fail := func(code int, format string, args ...any) ([]string, *Response) {
		return nil, withFile(NewErrorResponse(req.ID, code, "not saved: "+fmt.Sprintf(format, args...)), canonical)
	}
	id := w.Question.Query
	q, err := h.access.Queries().Get(ctx, id)
	switch {
	case errors.Is(err, queries.ErrNotFound):
		return fail(ErrInvalidParams, "the saved query %q does not exist (see list_queries). Save it first with save_query, then write the watch.", id)
	case err != nil:
		return fail(ErrInternal, "failed to read the saved query: %s", scrubStoreText(err.Error()))
	case q.Error != "":
		return fail(ErrInvalidParams, "the saved query %q cannot be read: %s", id, scrubStoreText(q.Error))
	}
	if _, err := queries.ParseValues(q, w.Question.Params); err != nil {
		return fail(ErrInvalidParams, "question.params does not fit saved query %q: %s", id, scrubFeedbackText(err.Error()))
	}
	var warnings []string
	for _, p := range q.Parameters {
		if _, given := w.Question.Params[p.Name]; !given && p.Required && p.Default == "" {
			warnings = append(warnings, fmt.Sprintf("saved query %q needs a value for parameter %q and has no default, so every check will be an error until question.params.%s is set", id, p.Name, p.Name))
		}
	}
	return warnings, nil
}

func (h *Handler) checkWatchMetric(ctx context.Context, req *Request, w *watches.Watch, canonical string) ([]string, *Response) {
	name := w.Question.Metric
	m, err := h.access.Semantics().Metric(ctx, name)
	switch {
	case errors.Is(err, semantics.ErrNotFound):
		return nil, withFile(NewErrorResponse(req.ID, ErrInvalidParams, fmt.Sprintf("not saved: there is no metric named %q (see list_definitions). A metric is proposed with propose_definition and takes effect when the person accepts it.", truncateRunesText(name, 80))), canonical)
	case err != nil:
		return nil, withFile(NewErrorResponse(req.ID, ErrInvalidParams, scrubStoreText(fmt.Sprintf("not saved: the metric %q cannot be read: %s", name, semantics.ReadableError(err)))), canonical)
	}
	if info := h.metricQueryInfo(ctx, m); info["query_error"] != nil {
		return []string{scrubStoreText(fmt.Sprintf("metric %q: %v, so every check will be an error until it is fixed", name, info["query_error"]))}, nil
	}
	return nil, nil
}

// watchAsks is the approval card's one-line description of a write_watch:
// ": query stuck_orders, fires when any rows". The request has not been
// validated yet when the card is built for a bad one, so it says nothing for a
// shape it cannot read.
func watchAsks(req *Request) string {
	q, qerr := questionFromParams(req.Params["question"])
	c, cerr := conditionFromParams(req.Params["when"])
	if qerr != nil || cerr != nil {
		return ""
	}
	what := "query " + q.Query
	if q.Metric != "" {
		what = "metric " + q.Metric
	}
	return fmt.Sprintf(": %s, fires when %s", what, c.Describe())
}
