package agents

import (
	"encoding/json"
	"fmt"
	"strings"
)

// viewShape/viewFit/bestView are a Go port of studio/frontend/src/lib/
// notebookViews.ts (itself ported from schoolwork-workspace's
// src/notebook/views.js) — the SAME view-fitting rules, kept in sync by
// hand since preview_query (Go, this file) and the notebook cell view picker
// (TypeScript) both need to agree on what "suggestedView" means for a given
// result shape. Any rule change here must be mirrored there and vice versa —
// see notebookViews.spec.ts, whose fixtures this file's tests reuse.

// viewShape describes a result set: which columns hold numbers, which hold
// labels, and whether the first column's values are unique (a GROUP BY key
// vs. a second measure).
type viewShape struct {
	cols          []string
	numeric       map[string]bool
	rowCount      int
	firstDistinct bool
	// long is set for a (label, series, value) result; see longViewShape.
	long *longViewShape
}

// longViewShape describes a (label, series, value) result: exactly three
// columns, a non-numeric second and a numeric third, what a GROUP BY label,
// series returns. Mirrors LongShape in notebookViews.ts.
type longViewShape struct {
	labels int
	series int
	// unique is true when no (label, series) pair repeats.
	unique bool
}

// isNumericValue judges a value from a Go-side result set (preview_query,
// run_query), which runs through engine.NormalizeValue: a small
// DECIMAL crosses as json.Number, not a Go float, so it must count as
// numeric too, or every DECIMAL column (which is most money/SUM columns)
// would stop fitting bar/line/stat and fall back to a label column instead.
// A json.Number OUT of the safe range crosses as a plain string instead
// (see the wire contract in engine/normalize.go) and is correctly judged a
// label here — that's the accepted "mixed column" edge case, not a bug.
func isNumericValue(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return true
	default:
		return false
	}
}

// describeViewShape judges a shape: from the declared column types when it has
// them (types[i] non-empty), else from the first non-null value, matching
// describeShape in notebookViews.ts.
func describeViewShape(cols []string, rows [][]any, types []string) viewShape {
	numeric := make(map[string]bool, len(cols))
	for ci, c := range cols {
		if ci < len(types) && types[ci] != "" {
			numeric[c] = isNumericTypeName(types[ci])
			continue
		}
		for _, r := range rows {
			if cellAt(r, ci) == nil {
				continue
			}
			numeric[c] = isNumericValue(r[ci])
			break
		}
	}
	seen := make(map[string]bool, len(rows))
	firstDistinct := len(rows) > 0
	for _, r := range rows {
		key := toDisplayString(cellAt(r, 0))
		if seen[key] {
			firstDistinct = false
		}
		seen[key] = true
	}
	return viewShape{cols: cols, numeric: numeric, rowCount: len(rows), firstDistinct: firstDistinct, long: describeLongShape(cols, numeric, rows)}
}

// cellAt is r[i], or nil when the row is shorter (a missing cell is a null).
func cellAt(r []any, i int) any {
	if i < len(r) {
		return r[i]
	}
	return nil
}

// isNumericTypeName mirrors typeFamily() in lib/formatCell.ts for the families
// a chart can plot: integer, decimal and float DuckDB types.
func isNumericTypeName(t string) bool {
	t = strings.ToUpper(t)
	switch {
	case strings.HasSuffix(t, "]"), strings.HasPrefix(t, "STRUCT"), strings.HasPrefix(t, "MAP"), strings.HasPrefix(t, "UNION"):
		return false
	case strings.HasPrefix(t, "DATE"), strings.HasPrefix(t, "TIME"), strings.HasPrefix(t, "INTERVAL"):
		return false
	case strings.HasPrefix(t, "DECIMAL"), strings.HasPrefix(t, "NUMERIC"), strings.Contains(t, "INT"):
		return true
	}
	return t == "DOUBLE" || t == "FLOAT" || t == "REAL"
}

// describeLongShape returns the (label, series, value) description of a
// result, or nil when it is not one. Same rule as isLongTriple() in
// a2ui/multiseries.ts and longShape() in notebookViews.ts: three columns, a
// text second and a numeric third; a column named series settles it, else no
// column may be named label or name and some label must carry two or more
// series (a one-to-one text column is an attribute, not a series).
func describeLongShape(cols []string, numeric map[string]bool, rows [][]any) *longViewShape {
	if len(cols) != 3 || numeric[cols[1]] || !numeric[cols[2]] {
		return nil
	}
	if !strings.EqualFold(cols[1], "series") {
		for _, c := range cols {
			if strings.EqualFold(c, "label") || strings.EqualFold(c, "name") {
				return nil
			}
		}
		per := map[string]map[string]bool{}
		multi := false
		for _, r := range rows {
			l, sr := toDisplayString(cellAt(r, 0)), toDisplayString(cellAt(r, 1))
			if per[l] == nil {
				per[l] = map[string]bool{}
			}
			per[l][sr] = true
			multi = multi || len(per[l]) >= 2
		}
		if !multi {
			return nil
		}
	}
	labels, series, pairs := map[string]bool{}, map[string]bool{}, map[[2]string]bool{}
	for _, r := range rows {
		l, sr := toDisplayString(cellAt(r, 0)), toDisplayString(cellAt(r, 1))
		labels[l], series[sr], pairs[[2]string{l, sr}] = true, true, true
	}
	return &longViewShape{labels: len(labels), series: len(series), unique: len(pairs) == len(rows)}
}

func toDisplayString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// viewFitResult mirrors notebookViews.ts's ViewFit.
type viewFitResult struct {
	OK  bool
	Why string
}

// checkViewFit reports whether view can render shape — same rules as
// viewFit() in notebookViews.ts.
func checkViewFit(view string, shape viewShape) viewFitResult {
	if len(shape.cols) == 0 {
		return viewFitResult{OK: false, Why: "the query returned no columns"}
	}
	switch view {
	case "table":
		return viewFitResult{OK: true}
	case "bar", "line", "area", "columns", "rings", "radar":
		return checkSeriesFit(view, shape)
	case "sankey", "heatmap":
		return checkTripleFit(view, shape)
	case "stat":
		if shape.rowCount == 0 {
			return viewFitResult{OK: false, Why: "needs one row — this query returned none"}
		}
		if shape.rowCount > 1 {
			return viewFitResult{OK: false, Why: "shows a single number, but this query returns more than one row — aggregate it, or add LIMIT 1"}
		}
		return viewFitResult{OK: true}
	default:
		return viewFitResult{OK: true}
	}
}

// checkSeriesFit is the rule for the label-then-numbers charts: a label
// column and at least one numeric column, plus radar's three-axis minimum and
// the rings' six-ring maximum.
func checkSeriesFit(view string, shape viewShape) viewFitResult {
	hasNumeric := false
	for _, n := range shape.numeric {
		if n {
			hasNumeric = true
			break
		}
	}
	if !hasNumeric {
		return viewFitResult{OK: false, Why: "needs a numeric column — this query returns only text (" + strings.Join(shape.cols, ", ") + ")"}
	}
	if len(shape.cols) < 2 {
		return viewFitResult{OK: false, Why: "needs two columns: something to label each point, then the number"}
	}
	if view == "radar" && shape.rowCount < 3 {
		return viewFitResult{OK: false, Why: "needs at least three rows, one per axis"}
	}
	if view == "rings" && shape.rowCount > maxRings {
		return viewFitResult{OK: false, Why: fmt.Sprintf("draws at most %d rings, but this query returns %d rows — filter or LIMIT it", maxRings, shape.rowCount)}
	}
	return viewFitResult{OK: true}
}

// maxRings is how many concentric rings the rings view draws.
const maxRings = 6

// checkTripleFit is the rule for sankey (source, target, weight) and heatmap
// (row, column, value): three columns, the third numeric.
func checkTripleFit(view string, shape viewShape) viewFitResult {
	want := "source, target, then a numeric weight"
	if view == "heatmap" {
		want = "a row label, a column label, then a numeric value"
	}
	if len(shape.cols) < 3 || !shape.numeric[shape.cols[2]] {
		return viewFitResult{OK: false, Why: "needs three columns: " + want}
	}
	return viewFitResult{OK: true}
}

var orderedNameHints = []string{"date", "day", "week", "month", "year", "term", "window", "period", "time", "quarter", "season"}

func looksOrderedName(name string) bool {
	n := strings.ToLower(name)
	for _, h := range orderedNameHints {
		if strings.Contains(n, h) {
			return true
		}
	}
	return false
}

const chartableRows = 25

// A (label, series, value) result is charted by bestViewFor only up to this
// many series and rows (the chart itself folds series past six into "Other").
// Mirrors MAX_LONG_SERIES and MAX_LONG_ROWS in notebookViews.ts.
const (
	maxLongSeries = 6
	maxLongRows   = 200
)

// bestViewFor is the conservative "which view suits this shape" pick — same
// rules as bestView() in notebookViews.ts. Prefers table whenever a chart
// would be a guess.
func bestViewFor(shape viewShape) string {
	if shape.rowCount == 1 && len(shape.cols) == 1 {
		for _, c := range shape.cols {
			if shape.numeric[c] {
				return "stat"
			}
		}
	}
	// (label, series, value): a line when the label reads as a sequence, else
	// grouped bars. Two to six series, at least two labels, no repeated pair.
	if l := shape.long; l != nil && l.unique && l.series >= 2 && l.series <= maxLongSeries &&
		l.labels >= 2 && shape.rowCount <= maxLongRows {
		if looksOrderedName(shape.cols[0]) {
			return "line"
		}
		if l.labels <= chartableRows {
			return "bar"
		}
		return "table"
	}
	if len(shape.cols) == 2 && shape.numeric[shape.cols[1]] && shape.rowCount > 1 &&
		shape.rowCount <= chartableRows && shape.firstDistinct {
		if looksOrderedName(shape.cols[0]) {
			return "line"
		}
		return "bar"
	}
	return "table"
}
