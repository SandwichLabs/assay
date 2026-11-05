package a2ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chartPage(typ, extra string) string {
	return `{"title":"x","components":[{"id":"c","type":"` + typ + `",` + extra + `}]}`
}

func TestValidate_AnnotationsAccepted(t *testing.T) {
	for _, typ := range []string{"lineChart", "areaChart", "columnChart"} {
		for _, extra := range []string{
			`"annotations":{"rows":[{"at":"2025-05-01","label":"Starter pack price cut"}]}`,
			`"annotations":{"rows":[{"at":"2025-05-01 10:30","label":"A","kind":"dashed","color":"error"},{"at":"Q2","label":"B","kind":"solid","color":"muted"},{"at":2024,"label":"C"}]}`,
			`"annotations":{"rows":[]}`,
			`"annotations":{"sql":"SELECT start_date AS at, event_name AS label FROM live_ops_events"}`,
			`"annotations":{"sql":"SELECT 1 AS at, 'x' AS label","connection":"gloamwick"}`,
			`"sql":"SELECT 1, 2","annotations":{"rows":[{"at":"2025-05","label":"month"}]}`,
		} {
			_, err := Validate(chartPage(typ, extra))
			require.NoError(t, err, typ+" "+extra)
		}
	}
}

func TestValidate_AnnotationsRejected(t *testing.T) {
	cases := []struct{ name, extra, want string }{
		{"not an object", `"annotations":[{"at":"2025-05-01","label":"x"}]`, "components[0].annotations: expected an object"},
		{"unknown field", `"annotations":{"rows":[],"until":"2025"}`, "components[0].annotations.until: annotations has no such field"},
		{"no source", `"annotations":{}`, "needs a source"},
		{"two sources", `"annotations":{"rows":[],"sql":"SELECT 1"}`, "use one source, got rows and sql"},
		{"query outside a view", `"annotations":{"query":"events"}`, "only allowed in a data artifact view"},
		{"connection alone", `"annotations":{"rows":[],"connection":"x"}`, "annotations.connection: only goes with sql"},
		{"connection type", `"annotations":{"sql":"SELECT 1","connection":4}`, "annotations.connection: expected string"},
		{"empty sql", `"annotations":{"sql":"  "}`, "annotations.sql: expected a non-empty SQL string"},
		{"rows not array", `"annotations":{"rows":"x"}`, "annotations.rows: expected an array"},
		{"marker not object", `"annotations":{"rows":["2025-01-01"]}`, "annotations.rows[0]: expected an object"},
		{"unknown marker field", `"annotations":{"rows":[{"at":"a","label":"b","until":"c"}]}`, "rows[0].until: a marker has no such field"},
		{"missing at", `"annotations":{"rows":[{"label":"b"}]}`, "rows[0].at: required"},
		{"null at", `"annotations":{"rows":[{"at":null,"label":"b"}]}`, "rows[0].at: required"},
		{"empty at", `"annotations":{"rows":[{"at":" ","label":"b"}]}`, "rows[0].at: a date or label of 1 to 80"},
		{"at object", `"annotations":{"rows":[{"at":{"d":1},"label":"b"}]}`, "rows[0].at: expected a date string or a category label, got object"},
		{"at bool", `"annotations":{"rows":[{"at":true,"label":"b"}]}`, "rows[0].at: expected a date string"},
		{"at multiline", "\"annotations\":{\"rows\":[{\"at\":\"a\\nb\",\"label\":\"b\"}]}", "rows[0].at: a date or label of 1 to 80"},
		{"at too long", `"annotations":{"rows":[{"at":"` + strings.Repeat("x", 81) + `","label":"b"}]}`, "rows[0].at: a date or label of 1 to 80"},
		{"impossible date", `"annotations":{"rows":[{"at":"2025-02-31","label":"b"}]}`, `rows[0].at: "2025-02-31" is not a real date`},
		{"month 13", `"annotations":{"rows":[{"at":"2025-13-01","label":"b"}]}`, "is not a real date"},
		{"hour 25", `"annotations":{"rows":[{"at":"2025-01-01 25:00","label":"b"}]}`, "is not a real date"},
		{"missing label", `"annotations":{"rows":[{"at":"2025-01-01"}]}`, "rows[0].label: required"},
		{"label type", `"annotations":{"rows":[{"at":"2025-01-01","label":3}]}`, "rows[0].label: expected string"},
		{"label empty", `"annotations":{"rows":[{"at":"2025-01-01","label":""}]}`, "rows[0].label: 1 to 80"},
		{"label too long", `"annotations":{"rows":[{"at":"2025-01-01","label":"` + strings.Repeat("l", 81) + `"}]}`, "rows[0].label: 1 to 80"},
		{"bad kind", `"annotations":{"rows":[{"at":"2025-01-01","label":"x","kind":"dotted"}]}`, `rows[0].kind: expected one of "solid", "dashed"`},
		{"bad color", `"annotations":{"rows":[{"at":"2025-01-01","label":"x","color":"#ff0000"}]}`, "rows[0].color: expected one of"},
	}
	for _, tc := range cases {
		_, err := Validate(chartPage("lineChart", tc.extra))
		require.Error(t, err, tc.name)
		assert.Contains(t, err.Error(), tc.want, tc.name)
	}
}

func TestValidate_AnnotationsCap(t *testing.T) {
	rows := func(n int) string {
		items := make([]string, n)
		for i := range items {
			items[i] = fmt.Sprintf(`{"at":"2025-01-%02d","label":"e%d"}`, i%28+1, i)
		}
		return `"annotations":{"rows":[` + strings.Join(items, ",") + `]}`
	}
	_, err := Validate(chartPage("columnChart", rows(50)))
	require.NoError(t, err)
	_, err = Validate(chartPage("columnChart", rows(51)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "51 markers is over the limit of 50")
}

func TestValidate_AnnotationsNotOnOtherTypes(t *testing.T) {
	for _, typ := range []string{"barChart", "ringChart", "radarChart", "table", "streamChart", "heatmap", "sankeyChart"} {
		_, err := Validate(chartPage(typ, `"annotations":{"rows":[]}`))
		require.Error(t, err, typ)
		assert.Contains(t, err.Error(), "components[0].annotations", typ)
	}
}

func TestValidateView_AnnotationsQuery(t *testing.T) {
	ok := func(extra string) {
		t.Helper()
		_, err := ValidateView(viewPage(`{"id":"c","type":"lineChart","query":"by_region",`+extra+`}`), testCtx)
		require.NoError(t, err, extra)
	}
	bad := func(extra, want string) {
		t.Helper()
		_, err := ValidateView(viewPage(`{"id":"c","type":"lineChart","query":"by_region",`+extra+`}`), testCtx)
		require.Error(t, err, extra)
		assert.Contains(t, err.Error(), want, extra)
	}
	ok(`"annotations":{"query":"customers"}`)
	ok(`"annotations":{"rows":[{"at":"2025-05-01","label":"x"}]}`)
	bad(`"annotations":{"query":"nope"}`, `components[0].annotations.query: "nope" is not a query of this artifact`)
	bad(`"annotations":{"query":3}`, "expected a query name")
	bad(`"annotations":{"sql":"SELECT 1"}`, "a data artifact view reads data through a named query")
	bad(`"annotations":{"query":"customers","rows":[]}`, "use one source, got query and rows")
	bad(`"annotations":{"query":"customers","connection":"x"}`, "only goes with sql")
	// columnChart and areaChart take it in a view too.
	for _, typ := range []string{"areaChart", "columnChart"} {
		_, err := ValidateView(viewPage(`{"id":"c","type":"`+typ+`","query":"by_region","annotations":{"query":"customers"}}`), testCtx)
		require.NoError(t, err, typ)
	}
	// ...and barChart does not.
	_, err := ValidateView(viewPage(`{"id":"c","type":"barChart","query":"by_region","annotations":{"query":"customers"}}`), testCtx)
	require.Error(t, err)
}

func TestValidateView_AnnotationsColumns(t *testing.T) {
	p, err := ValidateView(viewPage(`{"id":"c","type":"lineChart","query":"by_region","annotations":{"query":"customers"}}`), testCtx)
	require.NoError(t, err)
	got := CheckViewColumns(p, map[string][]string{"by_region": {"month", "rev"}, "customers": {"when"}})
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "annotations.query")
	assert.Contains(t, got[0], "at, label")
	assert.Empty(t, CheckViewColumns(p, map[string][]string{"by_region": {"month", "rev"}, "customers": {"at", "label"}}))
	assert.Empty(t, CheckViewColumns(p, map[string][]string{"by_region": {"month", "rev"}}))
}

func TestSkillBody_DocumentsAnnotations(t *testing.T) {
	body := SkillBody()
	assert.Contains(t, body, "## Event markers (annotations)")
	assert.Contains(t, body, "`lineChart`, `areaChart` and `columnChart`")
	assert.Contains(t, body, "At most 50 markers per chart")
	// the dashboard types say sql, the view types say query
	split := strings.Index(body, "## Data artifact view components")
	dash, view := body[:split], body[split:]
	assert.Contains(t, dash, "annotations({rows: [{at, label, kind, color}] | sql})")
	assert.Contains(t, view, "annotations({rows: [{at, label, kind, color}] | query})")
	assert.NotContains(t, view, "kind, color}] | sql})")
}

func TestPromptBlock_DocumentsAnnotations(t *testing.T) {
	block := PromptBlock()
	for _, typ := range []string{"lineChart", "areaChart", "columnChart"} {
		line := ""
		for _, l := range strings.Split(block, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), typ+" ") {
				line = l
			}
		}
		assert.Contains(t, line, "annotations({rows: [{at, label, kind, color}] | sql})", typ)
	}
}
