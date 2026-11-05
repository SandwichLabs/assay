// multiseries — the data shaping of lineChart and barChart. A chart is one
// series (the long-standing `(label, value)` result) or several: the long shape
// `(label, series, value)` that a GROUP BY label, series returns, the wide shape
// `(label, n1, n2, ...)` with one numeric column per series, or an inline
// `series: [{name, points}]`. Pure: no Vue, no bindings.
//
// The same detection runs for a live sql binding, a manifest query and inline
// columns + rows, and `isMultiSeries` is what the notebook cell uses to hand a
// result to the chart whole. internal/agents/notebook_views.go mirrors the long
// shape rule for the cell's suggested view; notebookViews.spec.ts and
// notebook_task_test.go pin the two together.
import { typeFamily } from '../lib/formatCell'
import { isNumeric } from './series'

// At most this many series are drawn. Past that, bars sum the smaller ones into
// one "Other"; lines draw the six largest and say how many are not shown (a sum
// of unrelated lines is not a line anyone asked for).
const MAX_SERIES = 6

// FoldMode: what happens to series past MAX_SERIES.
export type FoldMode = 'sum' | 'drop'

const OTHER = 'Other'

export interface SeriesLine {
  name: string
  // one entry per label; null where the series has no point (a gap, never zero)
  values: (number | null)[]
}

export interface MultiSeries {
  shape: 'single' | 'long' | 'wide' | 'inline'
  labels: string[]
  series: SeriesLine[]
  // names of the series summed into "Other" (mode 'sum') or not drawn (mode
  // 'drop'); empty when nothing folded
  folded: string[]
  foldMode: FoldMode
  // a (label, series) pair or a label was seen twice and summed, so a bar or
  // point is no longer one result row (drill-down is off)
  merged: boolean
}

export interface ResultTable {
  columns: string[]
  rows: any[][]
  types?: string[]
}

const EMPTY: MultiSeries = { shape: 'single', labels: [], series: [], folded: [], foldMode: 'sum', merged: false }

// cell reads a result cell as a number, or null for a null, a blank, a boolean
// or text. A HUGEINT/DECIMAL past JS's safe range crosses the wire as a numeric
// string, so one is read.
function cell(v: unknown): number | null {
  if (v === null || v === undefined || typeof v === 'boolean') return null
  return isNumeric(v) ? Number(v) : null
}

function columnNames(t: ResultTable): string[] {
  if (t.columns.length) return t.columns
  return t.rows[0] ? t.rows[0].map((_, i) => String(i)) : []
}

// numericColumns judges each column by its declared DuckDB type when the
// result has them, else by its first non-null value.
function numericColumns(t: ResultTable, n: number): boolean[] {
  return Array.from({ length: n }, (_, i) => {
    const dbType = t.types?.[i]
    if (dbType) {
      const f = typeFamily(dbType, undefined)
      return f === 'integer' || f === 'decimal' || f === 'float'
    }
    const sample = t.rows.find((r) => r[i] !== null && r[i] !== undefined)
    return sample !== undefined && isNumeric(sample[i])
  })
}

function indexOfName(cols: string[], ...names: string[]): number {
  for (const n of names) {
    const i = cols.findIndex((c) => c.toLowerCase() === n)
    if (i >= 0) return i
  }
  return -1
}

const text = (v: unknown): string => String(v ?? '')

// isLongTriple: is this three-column result (label, series, value)? The second
// column must be text and the third numeric. A column named `series` settles it.
// Otherwise a column named label or name makes the result a plain (label,
// value) chart with an attribute beside it (`SELECT id, name, total`), and some
// label has to carry two or more series: a one-to-one label to text column
// (`day, top_product, sales`) is an attribute, not a series. Shared with
// notebookViews.ts so the cell's suggested view agrees (and internal/agents).
export function isLongTriple(cols: string[], numeric: boolean[], rows: any[][]): boolean {
  if (cols.length !== 3 || numeric[1] || !numeric[2]) return false
  if (cols[1].toLowerCase() === 'series') return true
  if (indexOfName(cols, 'label', 'name') >= 0) return false
  const per = new Map<string, Set<string>>()
  for (const r of rows) {
    const l = text(r[0])
    let set = per.get(l)
    if (!set) per.set(l, (set = new Set()))
    set.add(text(r[1]))
    if (set.size >= 2) return true
  }
  return false
}

type Roles =
  | { kind: 'long'; label: number; series: number; value: number }
  | { kind: 'wide'; label: number; values: number[] }
  | { kind: 'single' }

function numericIndexes(numeric: boolean[], except: number[]): number[] {
  return numeric.map((n, i) => (n && !except.includes(i) ? i : -1)).filter((i) => i >= 0)
}

// namedLong: a column named `series` makes the result long. The value is the
// column named value/total, else the last numeric one; the label the column
// named label/name, else the first of the rest.
function namedLong(cols: string[], numeric: boolean[]): Roles | null {
  const series = indexOfName(cols, 'series')
  if (series < 0) return null
  let value = indexOfName(cols, 'value', 'total')
  if (value === series) value = -1
  if (value < 0) value = numericIndexes(numeric, [series]).pop() ?? -1
  let label = indexOfName(cols, 'label', 'name')
  if (label === series || label === value) label = -1
  if (label < 0) label = cols.findIndex((_, i) => i !== series && i !== value)
  return value >= 0 && label >= 0 ? { kind: 'long', label, series, value } : null
}

// roles decides what each column means:
//  1. a column named `series` (namedLong)
//  2. exactly three columns, a text second and a numeric third, are
//     (label, series, value) (isLongTriple), whatever the first holds
//  3. a column named value or total keeps the one-series reading it always had
//  4. a label then two or more numeric columns is wide
//  5. anything else is one series (label, value), as it always was
function roles(cols: string[], numeric: boolean[], rows: any[][]): Roles {
  const named = namedLong(cols, numeric)
  if (named) return named
  if (isLongTriple(cols, numeric, rows)) return { kind: 'long', label: 0, series: 1, value: 2 }
  if (indexOfName(cols, 'value', 'total') >= 0) return { kind: 'single' }
  const firstText = numeric.findIndex((n) => !n)
  const label = firstText < 0 ? 0 : firstText
  const values = numericIndexes(numeric, [label])
  return values.length >= 2 ? { kind: 'wide', label, values } : { kind: 'single' }
}

// isSeriesShape: is this result long or wide, so it has to go to the chart
// whole? (A long result filtered down to one series is still read by its series
// and value columns, not as label + second column.)
export function isSeriesShape(t: ResultTable): boolean {
  const cols = columnNames(t)
  return roles(cols, numericColumns(t, cols.length), t.rows).kind !== 'single'
}

function singlePoints(t: ResultTable, cols: string[], numeric: boolean[]): MultiSeries {
  const labelIdx = indexOfName(cols, 'label', 'name')
  const valueIdx = indexOfName(cols, 'value', 'total')
  const li = labelIdx >= 0 ? labelIdx : 0
  // the second column, unless it is text and a later column is numeric
  const second = cols.length > 1 ? 1 : 0
  const later = numeric.findIndex((n, i) => n && i !== li)
  const vi = valueIdx >= 0 ? valueIdx : numeric[second] || later < 0 ? second : later
  return {
    shape: 'single',
    labels: t.rows.map((r) => String(r[li] ?? '')),
    series: [{ name: cols[vi] ?? 'value', values: t.rows.map((r) => cell(r[vi]) ?? 0) }],
    folded: [],
    foldMode: 'sum',
    merged: false,
  }
}

// Grid collects (label, series) -> value, series in first-seen order. A
// repeated pair is summed: it is the same measure listed twice, as a result with
// no GROUP BY returns it (and then `merged` says a point is no longer one row).
//
// Label order merges the order each series saw its labels in, so
// `ORDER BY rep, month` still runs January to December even when the first rep
// has no January: a label a series introduces goes after the one before it in
// that series. All-numeric labels are sorted by value instead.
class Grid {
  names: string[] = []
  merged = false
  private labelList: string[] = []
  private li = new Map<string, number>()
  private si = new Map<string, number>()
  private at = new Map<string, number>()
  private seen: number[][] = []

  label(l: string): number {
    let i = this.li.get(l)
    if (i === undefined) this.li.set(l, (i = this.labelList.push(l) - 1))
    return i
  }
  name(s: string): number {
    let i = this.si.get(s)
    if (i === undefined) {
      this.si.set(s, (i = this.names.push(s) - 1))
      this.seen[i] = []
    }
    return i
  }
  add(l: number, s: number, v: number | null) {
    if (!this.seen[s].includes(l)) this.seen[s].push(l)
    if (v === null) return
    const k = `${l}:${s}`
    if (this.at.has(k)) this.merged = true
    this.at.set(k, (this.at.get(k) ?? 0) + v)
  }
  // label indexes in display order
  private order(): number[] {
    const all = this.labelList.map((_, i) => i)
    if (all.length > 0 && this.labelList.every((l) => isNumeric(l))) return all.sort((a, b) => Number(this.labelList[a]) - Number(this.labelList[b]) || a - b)
    const out: number[] = []
    for (const list of this.seen) {
      let anchor = -1
      for (const l of list) {
        const at = out.indexOf(l)
        if (at >= 0) anchor = at
        else out.splice(++anchor, 0, l)
      }
    }
    return out
  }
  get labelCount(): number {
    return this.labelList.length
  }
  get labels(): string[] {
    return this.order().map((l) => this.labelList[l])
  }
  lines(): SeriesLine[] {
    const ord = this.order()
    return this.names.map((name, s) => ({ name, values: ord.map((l) => this.at.get(`${l}:${s}`) ?? null) }))
  }
}

// foldSeries limits a chart to MAX_SERIES series; up to six are returned as they are.
//
// 'drop' (lines) keeps the six with the largest total magnitude, ties in their
// original order, drawn in their original order; the rest are named in `folded`
// and not drawn. A series called "Other" is an ordinary series there.
//
// 'sum' (bars) keeps the five largest and sums the rest, and any series already
// named "Other", into one "Other" per label (a gap only where every folded
// series has one).
export function foldSeries(series: SeriesLine[], mode: FoldMode = 'sum'): { series: SeriesLine[]; folded: string[] } {
  if (series.length <= MAX_SERIES) return { series, folded: [] }
  const total = (s: SeriesLine) => s.values.reduce<number>((a, v) => a + Math.abs(v ?? 0), 0)
  if (mode === 'drop') {
    const top = new Set(
      series
        .map((s, i) => ({ i, t: total(s) }))
        .sort((a, b) => b.t - a.t || a.i - b.i)
        .slice(0, MAX_SERIES)
        .map((r) => r.i),
    )
    return { series: series.filter((_, i) => top.has(i)), folded: series.filter((_, i) => !top.has(i)).map((s) => s.name) }
  }
  const ranked = series
    .map((s, i) => ({ i, t: total(s), other: s.name.trim().toLowerCase() === OTHER.toLowerCase() }))
    .filter((r) => !r.other)
    .sort((a, b) => b.t - a.t || a.i - b.i)
  const keep = new Set(ranked.slice(0, MAX_SERIES - 1).map((r) => r.i))
  const rest = series.filter((_, i) => !keep.has(i))
  const other: SeriesLine = {
    name: OTHER,
    values: series[0].values.map((_, l) => {
      const present = rest.map((s) => s.values[l]).filter((v): v is number => v !== null)
      return present.length ? present.reduce((a, v) => a + v, 0) : null
    }),
  }
  return { series: [...series.filter((_, i) => keep.has(i)), other], folded: rest.map((s) => s.name) }
}

function finish(shape: MultiSeries['shape'], g: Grid, mode: FoldMode): MultiSeries {
  const f = foldSeries(g.lines(), mode)
  return { shape, labels: g.labels, series: f.series, folded: f.folded, foldMode: mode, merged: g.merged }
}

// toMultiSeries shapes a result (or inline columns + rows) into labels and series.
export function toMultiSeries(t: ResultTable, mode: FoldMode = 'sum'): MultiSeries {
  const cols = columnNames(t)
  if (t.rows.length === 0 || cols.length === 0) return EMPTY
  const numeric = numericColumns(t, cols.length)
  const r = roles(cols, numeric, t.rows)
  if (r.kind === 'single') return singlePoints(t, cols, numeric)
  const g = new Grid()
  if (r.kind === 'long') {
    for (const row of t.rows) {
      const name = String(row[r.series] ?? '')
      g.add(g.label(String(row[r.label] ?? '')), g.name(name === '' ? '(blank)' : name), cell(row[r.value]))
    }
    return finish('long', g, mode)
  }
  const idx = r.values.map((c) => g.name(cols[c]))
  for (const row of t.rows) {
    const before = g.labelCount
    const l = g.label(String(row[r.label] ?? ''))
    if (g.labelCount === before) g.merged = true
    r.values.forEach((c, k) => g.add(l, idx[k], cell(row[c])))
  }
  return finish('wide', g, mode)
}

export interface InlineSeries {
  name: string
  points: { label: string; value: number }[]
}

// fromInlineSeries shapes a lineChart's `series: [{name, points}]`: the union of
// the series' labels, in first-seen order, with a gap where a series lacks one.
export function fromInlineSeries(list: InlineSeries[], mode: FoldMode = 'sum'): MultiSeries {
  const g = new Grid()
  for (const s of list) {
    const si = g.name(s.name)
    for (const p of s.points ?? []) g.add(g.label(String(p.label ?? '')), si, cell(p.value))
  }
  return finish('inline', g, mode)
}

// fromPoints is the single-series inline form (`points`, `bars`).
export function fromPoints(points: { label: string; value: number }[]): MultiSeries {
  return {
    shape: 'single',
    labels: points.map((p) => String(p.label ?? '')),
    series: [{ name: 'value', values: points.map((p) => cell(p.value) ?? 0) }],
    folded: [],
    foldMode: 'sum',
    merged: false,
  }
}
