package a2ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SandwichLabs/slaw/internal/jsonenc"
)

// Validation of a chart's `annotations`: event markers (a vertical rule and a
// short label) on lineChart, areaChart and columnChart. barChart is left out,
// its bars are horizontal and a vertical rule would mean nothing there.
//
//	"annotations": {"rows": [{"at": "2025-05-01", "label": "Price cut", "kind": "dashed", "color": "error"}]}
//	"annotations": {"sql": "SELECT start_date AS at, name AS label FROM events", "connection": "prod"}
//	"annotations": {"query": "events"}                      (a data artifact view)
//
// The object is closed: exactly one source, a closed marker shape, the shared
// colour enum, and at most maxAnnotations markers. Where a marker lands (a
// date on a time axis, a label on a category axis) is decided at render time,
// since the axis is only known once the data is.

const (
	// maxAnnotations caps the markers on one chart, inline or from sql.
	maxAnnotations = 50
	// maxAnnotationText bounds `at` and `label`: both render as one line of text.
	maxAnnotationText = 80
)

var annotationKindSpec = fieldSpec{kind: kindEnum, values: []any{"solid", "dashed"}}

// annotationShape matches what the chart's time axis reads as a date or
// timestamp (scale.ts TIME_RE). An `at` of this shape must be a real date.
var annotationShape = regexp.MustCompile(`^(\d{4})-(\d{2})(?:-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?)?)?(?:Z|[+-]\d{2}(?::?\d{2})?)?$`)

func checkAnnotations(vc *ViewContext, value any, path string, errs *[]string) {
	obj, ok := value.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected an object like {rows: [{at, label}]} or {sql}, got %s", path, typeName(value)))
		return
	}
	var sources []string
	for _, key := range sortedKeys(obj) {
		switch key {
		case "rows", "sql", "connection", "query":
		default:
			*errs = append(*errs, fmt.Sprintf("%s.%s: annotations has no such field. Allowed: rows, sql, connection (query in a data artifact view)", path, key))
			continue
		}
		if key != "connection" {
			sources = append(sources, key)
		}
	}
	switch {
	case len(sources) > 1:
		*errs = append(*errs, fmt.Sprintf("%s: use one source, got %s", path, strings.Join(sources, " and ")))
	case len(sources) == 0:
		*errs = append(*errs, fmt.Sprintf("%s: needs a source: rows (inline markers), sql or, in a data artifact view, query", path))
	}
	if v, ok := obj["rows"]; ok {
		checkAnnotationRows(v, path+".rows", errs)
	}
	checkAnnotationBinding(vc, obj, path, errs)
}

// checkAnnotationBinding checks the live source: sql (+ connection) outside a
// view, a manifest query name inside one. Same split as the chart's own fields.
func checkAnnotationBinding(vc *ViewContext, obj map[string]any, path string, errs *[]string) {
	if v, ok := obj["sql"]; ok {
		if vc != nil {
			*errs = append(*errs, fmt.Sprintf("%s.sql: a data artifact view reads data through a named query: use query", path))
		} else if s, isStr := v.(string); !isStr || strings.TrimSpace(s) == "" {
			*errs = append(*errs, fmt.Sprintf("%s.sql: expected a non-empty SQL string, got %s", path, typeName(v)))
		}
	}
	if v, ok := obj["connection"]; ok {
		if _, has := obj["sql"]; !has {
			*errs = append(*errs, fmt.Sprintf("%s.connection: only goes with sql", path))
		} else if vc == nil {
			checkValue(connSpec, v, path+".connection", errs)
		}
	}
	if v, ok := obj["query"]; ok {
		if vc == nil {
			*errs = append(*errs, fmt.Sprintf("%s.query: query is only allowed in a data artifact view. Use rows or sql", path))
		} else {
			checkQueryName(vc, v, path+".query", errs)
		}
	}
}

func checkAnnotationRows(value any, path string, errs *[]string) {
	arr, ok := value.([]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected an array of {at, label}, got %s", path, typeName(value)))
		return
	}
	if len(arr) > maxAnnotations {
		*errs = append(*errs, fmt.Sprintf("%s: %d markers is over the limit of %d. Keep the ones that matter, or use sql with a WHERE or LIMIT", path, len(arr), maxAnnotations))
		return
	}
	for i, item := range arr {
		checkOneAnnotation(item, fmt.Sprintf("%s[%d]", path, i), errs)
	}
}

func checkOneAnnotation(item any, p string, errs *[]string) {
	obj, ok := item.(map[string]any)
	if !ok {
		*errs = append(*errs, fmt.Sprintf("%s: expected an object like {at, label}, got %s", p, typeName(item)))
		return
	}
	for _, key := range sortedKeys(obj) {
		switch key {
		case "at", "label", "kind", "color":
		default:
			*errs = append(*errs, fmt.Sprintf("%s.%s: a marker has no such field. Allowed: at, label, kind, color", p, key))
		}
	}
	checkAnnotationAt(obj["at"], p+".at", errs)
	checkAnnotationLabel(obj["label"], p+".label", errs)
	if v, ok := obj["kind"]; ok {
		checkValue(annotationKindSpec, v, p+".kind", errs)
	}
	if v, ok := obj["color"]; ok {
		checkValue(colorSpec, v, p+".color", errs)
	}
}

// oneLine reports whether s is 1..maxAnnotationText valid characters on one line.
func oneLine(s string) bool {
	n := utf8.RuneCountInString(s)
	return strings.TrimSpace(s) != "" && n <= maxAnnotationText && utf8.ValidString(s) && !strings.ContainsAny(s, "\n\r\x00")
}

func checkAnnotationAt(v any, p string, errs *[]string) {
	switch at := v.(type) {
	case nil:
		*errs = append(*errs, fmt.Sprintf("%s: required: a date or timestamp on a time axis, a category label otherwise", p))
	case string:
		switch {
		case !oneLine(at):
			*errs = append(*errs, fmt.Sprintf("%s: a date or label of 1 to %d characters on one line", p, maxAnnotationText))
		case !realDateIfDateShaped(strings.TrimSpace(at)):
			*errs = append(*errs, fmt.Sprintf("%s: %s is not a real date", p, jsonenc.Quote(at)))
		}
	default:
		if !isFiniteNumber(v) {
			*errs = append(*errs, fmt.Sprintf("%s: expected a date string or a category label, got %s", p, typeName(v)))
		}
	}
}

func checkAnnotationLabel(v any, p string, errs *[]string) {
	s, ok := v.(string)
	switch {
	case v == nil:
		*errs = append(*errs, fmt.Sprintf("%s: required: the short text shown beside the marker", p))
	case !ok:
		*errs = append(*errs, fmt.Sprintf("%s: expected string, got %s", p, typeName(v)))
	case !oneLine(s):
		*errs = append(*errs, fmt.Sprintf("%s: 1 to %d characters on one line", p, maxAnnotationText))
	}
}

// realDateIfDateShaped is true for text that does not look like a date, and for
// a date-shaped text only when it names a real date and time ("2025-02-31" and
// "2025-13-01" are typos, not category labels).
func realDateIfDateShaped(s string) bool {
	m := annotationShape.FindStringSubmatch(s)
	if m == nil {
		return true
	}
	n := func(i int, def int) int {
		if m[i] == "" {
			return def
		}
		v, _ := strconv.Atoi(m[i])
		return v
	}
	y, mo, d, h, mi, sec := n(1, 0), n(2, 1), n(3, 1), n(4, 0), n(5, 0), n(6, 0)
	if y < 100 || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || sec > 59 {
		return false
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, 0, time.UTC)
	return int(t.Month()) == mo && t.Day() == d
}
