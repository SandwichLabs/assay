package a2ui

import "fmt"

// annotationsSection is the part of the a2ui skill about event markers. It is
// where the detail lives: the chat prompt only carries the field's shape.
func annotationsSection() string {
	return fmt.Sprintf(`## Event markers (annotations)

Use them for "mark each release / price change / incident / live-ops event" and
"did change X move metric Y?": a vertical rule at each date, with its label. Only
%s take them; a barChart's bars are horizontal, so it does not.

    "annotations": {"rows": [{"at": "2025-05-01", "label": "Starter pack price cut"},
                             {"at": "2025-06-12", "label": "Double XP", "kind": "dashed", "color": "error"}]}
    "annotations": {"sql": "SELECT start_date AS at, event_name AS label, kind FROM live_ops_events",
                    "connection": "gloamwick"}
    "annotations": {"query": "events"}          (data artifact view only; sql is refused there)

- One source: inline "rows", or "sql" (+ "connection"), or in an artifact view "query". The
  sql is a second, read-only live binding with the chart's own rules; the query returns
  (at, label[, kind, color]) by those column names, or by position (at, label). Use it when
  the events live in a table; otherwise inline rows.
- "at" is a date or timestamp ("2025-05-01", "2025-05-01 10:30") on a date axis, or a
  category label on a category axis. A date between two points of a weekly series is drawn
  at its exact time, with the exact date in the tooltip. A date-shaped "at" must be a real date.
- "label" is short (up to %d characters). "kind" is solid (default) or dashed: use it
  to tell two classes of event apart (say permanent changes solid, limited-time ones
  dashed); do not invent kinds. "color" is the usual closed set; the default is muted.
  Leave the colours alone unless the user asked for emphasis.
- At most %d markers per chart. More inline is refused; more from sql are dropped, and the chart
  says so. A marker outside the chart's x range is not drawn, and the chart says how many.
  Close dates are stacked into rows above the plot; a crowded chart hides some labels (the
  rules and tooltips stay), so keep to the events that matter.
- They combine with several series: one line per series and one set of markers across all of them.
`, "`lineChart`, `areaChart` and `columnChart`", maxAnnotationText, maxAnnotations)
}
