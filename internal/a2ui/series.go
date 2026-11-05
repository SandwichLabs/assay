package a2ui

import (
	"fmt"
	"strings"

	"github.com/SandwichLabs/slaw/internal/jsonenc"
)

// Validation of the inputs lineChart and barChart share for several series:
// the inline `series` list and the one-data-source rule.

// checkOneDataSource refuses a line or bar chart that names two data sources
// when one of them is the inline `series` or `rows` (the shapes added for several
// series): which one wins would be a guess. points or bars together with sql
// predates them and still passes.
func checkOneDataSource(obj map[string]any, path string, errs *[]string) {
	var present []string
	for _, f := range []string{"points", "bars", "series", "rows", "sql", "query"} {
		if _, ok := obj[f]; ok {
			present = append(present, f)
		}
	}
	_, hasSeries := obj["series"]
	_, hasRows := obj["rows"]
	if len(present) > 1 && (hasSeries || hasRows) {
		*errs = append(*errs, fmt.Sprintf("%s: use one data source, got %s", path, strings.Join(present, " and ")))
	}
}

// Caps on a lineChart's inline `series`. The chart draws six and sums the rest
// into "Other", so a few more than six is a reasonable query result; hundreds
// of series is a model that has gone off the rails.
const (
	maxInlineSeries       = 12
	maxInlineSeriesPoints = 500
	maxSeriesName         = 80
)

// checkSeriesList validates `series: [{name, points: [{label, value}]}]`: a
// closed object per series, a short non-empty name, and points shaped like the
// single-series `points`.
func checkSeriesList(value any, path string, errs *[]string) {
	arr, ok := value.([]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected array of {name, points}, got %s", path, typeName(value)))
		return
	}
	if len(arr) > maxInlineSeries {
		*errs = append(*errs, fmt.Sprintf("%s: %d series is over the limit of %d. Return one row per label and series and use sql, or merge the small ones", path, len(arr), maxInlineSeries))
		return
	}
	seen := map[string]bool{}
	for i, item := range arr {
		checkOneSeries(item, fmt.Sprintf("%s[%d]", path, i), seen, errs)
	}
}

func checkOneSeries(item any, p string, seen map[string]bool, errs *[]string) {
	obj, ok := item.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected an object like {name, points}, got %s", p, typeName(item)))
		return
	}
	for _, key := range sortedKeys(obj) {
		if key != "name" && key != "points" {
			*errs = append(*errs, fmt.Sprintf("%s.%s: a series has no such field. Allowed: name, points", p, key))
		}
	}
	name, isStr := obj["name"].(string)
	switch {
	case !isStr || strings.TrimSpace(name) == "":
		*errs = append(*errs, fmt.Sprintf("%s.name: expected a non-empty string, got %s", p, typeName(obj["name"])))
	case len([]rune(name)) > maxSeriesName:
		*errs = append(*errs, fmt.Sprintf("%s.name: longer than %d characters", p, maxSeriesName))
	case seen[strings.TrimSpace(name)]:
		*errs = append(*errs, fmt.Sprintf("%s.name: %s is used by an earlier series", p, jsonenc.Quote(name)))
	}
	seen[strings.TrimSpace(name)] = true
	pts, isArr := obj["points"].([]any)
	switch {
	case !isArr:
		*errs = append(*errs, fmt.Sprintf("%s.points: expected array, got %s", p, typeName(obj["points"])))
	case len(pts) > maxInlineSeriesPoints:
		*errs = append(*errs, fmt.Sprintf("%s.points: %d points is over the limit of %d", p, len(pts), maxInlineSeriesPoints))
	default:
		checkValue(pointArraySpec, pts, p+".points", errs)
	}
}
