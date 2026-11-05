// annotations — event markers on a lineChart, areaChart or columnChart: a
// vertical rule at a date (time axis) or a category, with a short label.
//
// This file is the pure part: reading the markers (inline rows or a query
// result), the 50-marker cap, where each lands on the chart's x axis (and which
// are outside it), and how the labels are staggered so close dates do not
// collide. useAnnotations.ts wires it to a chart and A2UIAnnotations.vue draws
// it. Colours are the closed enum (a2ui--<token> classes), never a model string
// in a style.
import { parseTime } from './scale'
import type { XAxis } from './xaxis'

export const MAX_ANNOTATIONS = 50

type AnnotationKind = 'solid' | 'dashed'
type AnnotationColor = 'yellow' | 'black' | 'error' | 'muted'
const KINDS: readonly string[] = ['solid', 'dashed']
const COLORS: readonly string[] = ['yellow', 'black', 'error', 'muted']

export interface Annotation {
  at: string
  label: string
  kind: AnnotationKind
  color: AnnotationColor
}

export interface ParsedAnnotations {
  items: Annotation[]
  // markers past MAX_ANNOTATIONS, dropped
  over: number
  // rows with no usable `at` or `label`
  skipped: number
  // sql rows whose kind or color was not in the closed set (drawn solid / muted)
  unknown: number
  // rows whose at or label was cut to MAX_TEXT characters (or had a line break turned into a space)
  clipped: number
}

// cell reads one value as trimmed text, or '' when it is empty, null or not text.
function cell(v: unknown): string {
  if (typeof v === 'string') return v.trim()
  if (typeof v === 'number' && Number.isFinite(v)) return String(v)
  if (v instanceof Date && !Number.isNaN(v.getTime())) return v.toISOString()
  return ''
}

const MAX_TEXT = 80

// oneLine: line breaks become spaces and the text is cut to MAX_TEXT characters,
// as the validator's limit for inline rows (a query result is not validated).
function oneLine(s: string): { text: string; changed: boolean } {
  const flat = s.replace(/[\r\n\0]+/g, ' ').trim()
  const chars = [...flat]
  const text = chars.length > MAX_TEXT ? chars.slice(0, MAX_TEXT - 1).join('') + '…' : flat
  return { text, changed: text !== s }
}

function build(at: unknown, label: unknown, kind: unknown, color: unknown, out: ParsedAnnotations): void {
  const a0 = oneLine(cell(at))
  const l0 = oneLine(cell(label))
  const a = a0.text
  const l = l0.text
  if (!a || !l) {
    out.skipped++
    return
  }
  if (a0.changed || l0.changed) out.clipped++
  const k = cell(kind)
  const c = cell(color)
  if ((k && !KINDS.includes(k)) || (c && !COLORS.includes(c))) out.unknown++
  out.items.push({
    at: a,
    label: l,
    kind: KINDS.includes(k) ? (k as AnnotationKind) : 'solid',
    color: COLORS.includes(c) ? (c as AnnotationColor) : 'muted',
  })
}

function capped(p: ParsedAnnotations): ParsedAnnotations {
  if (p.items.length > MAX_ANNOTATIONS) {
    p.over = p.items.length - MAX_ANNOTATIONS
    p.items = p.items.slice(0, MAX_ANNOTATIONS)
  }
  return p
}

const empty = (): ParsedAnnotations => ({ items: [], over: 0, skipped: 0, unknown: 0, clipped: 0 })

// fromRows reads the inline `rows: [{at, label, kind?, color?}]`.
export function fromRows(rows: unknown): ParsedAnnotations {
  const out = empty()
  if (!Array.isArray(rows)) return out
  for (const r of rows) {
    if (r && typeof r === 'object') build((r as any).at, (r as any).label, (r as any).kind, (r as any).color, out)
    else out.skipped++
  }
  return capped(out)
}

// fromResult reads a query result: the columns named at, label, kind and color,
// and, when at or label is not named, the first and second column.
export function fromResult(res: { columns: string[]; rows: unknown[][] }): ParsedAnnotations {
  const out = empty()
  const cols = res.columns.map((c) => c.toLowerCase())
  const at = cols.includes('at') ? cols.indexOf('at') : 0
  let label = cols.indexOf('label')
  if (label < 0) label = at === 0 ? 1 : 0
  const kind = cols.indexOf('kind')
  const color = cols.indexOf('color')
  for (const r of res.rows) build(r[at], r[label], kind >= 0 ? r[kind] : '', color >= 0 ? r[color] : '', out)
  return capped(out)
}

// ---- where a marker lands ----

export interface PlacedMarker {
  key: number
  ann: Annotation
  // 0..1 across the plot
  frac: number
  // the index of the chart label it belongs to (the bucket it falls in), for the tooltip
  bucket: number
}

export interface Placement {
  markers: PlacedMarker[]
  // markers not on this chart's x axis (dropped)
  outside: number
}

// placeOnAxis: a line or area chart. On a time axis a marker sits at its exact
// time (between two weekly points when it falls between them), belongs to the
// last point at or before it, and is dropped outside the first and last label.
// On a category axis `at` must name a label.
export function placeOnAxis(items: Annotation[], labels: string[], xa: XAxis): Placement {
  const markers: PlacedMarker[] = []
  let outside = 0
  items.forEach((ann, key) => {
    const hit = xa.axis ? onTimeAxis(ann, xa) : onCategory(ann, labels, (i) => xa.frac[i])
    if (hit) markers.push({ key, ann, ...hit })
    else outside++
  })
  return { markers, outside }
}

function onTimeAxis(ann: Annotation, xa: XAxis): { frac: number; bucket: number } | null {
  const ax = xa.axis!
  const t = parseTime(ann.at)
  if (t === null || t < ax.min || t > ax.max) return null
  let bucket = xa.order[0]
  for (const i of xa.order) if (ax.times[i] <= t) bucket = i
  return { frac: (t - ax.min) / (ax.max - ax.min), bucket }
}

function onCategory(ann: Annotation, labels: string[], fracOf: (i: number) => number): { frac: number; bucket: number } | null {
  const i = labels.findIndex((l) => l.trim() === ann.at)
  return i < 0 ? null : { frac: fracOf(i), bucket: i }
}

// placeOnColumns: a column chart. A marker sits on the column centre of the label
// it names. When the columns are dates in order (and `buckets` allows it: not
// for columns sorted by size), a date between two column dates sits on the
// column it falls in, up to one column past the last.
export function placeOnColumns(items: Annotation[], labels: string[], buckets: boolean): Placement {
  const n = labels.length
  const centre = (i: number) => (i + 0.5) / n
  const times = buckets ? labels.map((l) => parseTime(l)) : []
  const dated = buckets && n > 1 && times.every((t, i) => t !== null && (i === 0 || t > (times[i - 1] as number)))
  const reach = dated ? Math.max(...times.slice(1).map((t, i) => (t as number) - (times[i] as number))) : 0
  const markers: PlacedMarker[] = []
  let outside = 0
  items.forEach((ann, key) => {
    let hit = onCategory(ann, labels, centre)
    const t = dated && !hit ? parseTime(ann.at) : null
    if (t !== null && t >= (times[0] as number) && t < (times[n - 1] as number) + reach) {
      let i = 0
      while (i + 1 < n && (times[i + 1] as number) <= t) i++
      hit = { frac: centre(i), bucket: i }
    }
    if (hit) markers.push({ key, ann, ...hit })
    else outside++
  })
  return { markers, outside }
}

// ---- label layout ----

export const LANE_PX = 15
export const MAX_LANES = 4
const CHAR_PX = 6.6
const CHIP_PAD = 9

export interface LaidMarker extends PlacedMarker {
  // 0 is the row nearest the plot; null: no room, the rule is drawn without its label
  lane: number | null
  text: string
  // label chip left edge, px from the marker's own x (<= 0 when shifted left to stay inside the plot)
  shift: number
  // the width the chip was laid out with
  width: number
}

export interface Layout {
  markers: LaidMarker[]
  // rows of labels above the plot
  lanes: number
  // labels left out for lack of room
  hidden: number
}

function clip(label: string, max: number): string {
  const chars = [...label]
  return chars.length <= max ? label : chars.slice(0, max - 1).join('') + '…'
}

interface Box {
  m: PlacedMarker
  x: number
  a: number
  b: number
  text: string
  lane: number | null
}

// layoutMarkers staggers the labels into rows above the plot so none overlaps
// another label or is crossed by another marker's rule. The chip starts at its
// rule and runs right; near the right edge it shifts left to stay inside. A
// marker's rule reaches up to its own row, so a marker whose rule would pass
// through another's label gets a lower row than that label; with the
// markers laid out right to left that is rarely a constraint, and when no row
// of MAX_LANES fits, the label is dropped (the rule and its tooltip stay).
export function layoutMarkers(markers: PlacedMarker[], widthPx: number): Layout {
  const width = widthPx > 0 ? widthPx : 560
  const maxChars = Math.max(8, Math.min(28, Math.floor((width * 0.4) / CHAR_PX)))
  const boxes: Box[] = markers.map((m) => {
    const text = clip(m.ann.label, maxChars)
    const w = [...text].length * CHAR_PX + CHIP_PAD
    const x = m.frac * width
    const a = Math.max(0, Math.min(x, width - w))
    return { m, x, a, b: a + w, text, lane: null }
  })
  // Which labels get a row: earlier rows win (so ORDER BY decides who is kept),
  // then those are laid out right to left, which packs them best.
  let keep: Box[] = []
  for (const box of [...boxes].sort((p, q) => p.m.key - q.m.key)) {
    if (packRightToLeft([...keep, box])) keep.push(box)
  }
  const placed = packRightToLeft(keep) ? keep : (keep = [])
  let hidden = 0
  for (const box of boxes) {
    if (placed.includes(box)) continue
    box.lane = null
    hidden++
  }
  const lanes = placed.reduce((mx, b) => Math.max(mx, (b.lane as number) + 1), 0)
  return {
    markers: boxes.map((b) => ({ ...b.m, lane: b.lane, text: b.text, shift: b.a - b.x, width: b.b - b.a })),
    lanes,
    hidden,
  }
}

// packRightToLeft gives every box a row, right to left (which packs best), or
// reports that they do not all fit in MAX_LANES rows.
function packRightToLeft(set: Box[]): boolean {
  const placed: Box[] = []
  for (const box of [...set].sort((p, q) => q.x - p.x || q.m.key - p.m.key)) {
    box.lane = firstFreeLane(box, placed)
    if (box.lane === null) return false
    placed.push(box)
  }
  return true
}

// firstFreeLane: the lowest row where box's label overlaps no placed label, its rule
// crosses none (a rule reaches every row below its own), and no placed rule
// runs through it.
function firstFreeLane(box: Box, placed: Box[]): number | null {
  const PAD = 2
  for (let lane = 0; lane < MAX_LANES; lane++) {
    const ok = placed.every((o) => {
      const olane = o.lane as number
      const overlap = box.a < o.b + PAD && o.a < box.b + PAD
      // rules this close are one line to the eye: only the labels must not overlap
      const together = Math.abs(box.x - o.x) <= 2.5
      const myRuleInOther = !together && box.x >= o.a - PAD && box.x <= o.b + PAD
      const otherRuleInMine = !together && o.x >= box.a - PAD && o.x <= box.b + PAD
      if (myRuleInOther && lane >= olane) return false
      if (otherRuleInMine && olane >= lane) return false
      return !(overlap && lane === olane)
    })
    if (ok) return lane
  }
  return null
}

// ---- text ----

// describe: one marker as a sentence for the tooltip and the aria label.
export function describe(m: { ann: Annotation }): string {
  const { ann } = m
  return ann.label.trim() === ann.at ? ann.label : `${ann.label} (${ann.at})`
}

export interface NoteInput {
  over: number
  skipped: number
  unknown: number
  // rows whose at or label was cut to MAX_TEXT characters (or had a line break turned into a space)
  clipped: number
  outside: number
  hidden: number
  error?: string
}

// noteText: what the chart tells the reader about markers it did not draw as asked.
export function noteText(n: NoteInput): string {
  const parts: string[] = []
  const plural = (c: number, one: string, many: string) => `${c} ${c === 1 ? one : many}`
  if (n.error) parts.push(`Event markers: query error: ${n.error}`)
  if (n.outside) parts.push(`${plural(n.outside, 'marker is', 'markers are')} outside this chart's range and not shown`)
  if (n.over) parts.push(`${plural(n.over, 'marker', 'markers')} past the limit of ${MAX_ANNOTATIONS} not shown`)
  if (n.skipped) parts.push(`${plural(n.skipped, 'row', 'rows')} without a usable at and label skipped`)
  if (n.clipped) parts.push(`${plural(n.clipped, 'marker has', 'markers have')} a date or label cut to 80 characters or to one line`)
  if (n.unknown) parts.push(`${plural(n.unknown, 'marker has', 'markers have')} a kind or color other than solid, dashed, yellow, black, error, muted (drawn solid and muted)`)
  if (n.hidden) parts.push(`${plural(n.hidden, 'label is', 'labels are')} hidden to avoid overlap; hover a rule to read it`)
  return parts.join('. ')
}

// ariaText: the markers for the chart's text alternative.
export function ariaText(markers: PlacedMarker[]): string {
  if (markers.length === 0) return ''
  const ordered = [...markers].sort((a, b) => a.frac - b.frac)
  return `. ${markers.length} event ${markers.length === 1 ? 'marker' : 'markers'}: ${ordered.map(describe).join('; ')}`
}
