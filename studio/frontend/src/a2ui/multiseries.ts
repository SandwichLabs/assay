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

// At most this many series are drawn. More fold into one "Other".
const MAX_SERIES = 6
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
  // names of the series summed into "Other" (empty when nothing folded)
  folded: string[]
}

export interface ResultTable {
  columns: string[]
  rows: any[][]
  types?: string[]
}

const EMPTY: MultiSeries = { shape: 'single', labels: [], series: [], folded: [] }

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
//     (label, series, value), whatever the first holds (a year is numeric)
//  3. a column named value or total keeps the one-series reading it always had
//  4. a label then two or more numeric columns is wide
//  5. anything else is one series (label, value), as it always was
function roles(cols: string[], numeric: boolean[]): Roles {
  const named = namedLong(cols, numeric)
  if (named) return named
  if (cols.length === 3 && !numeric[1] && numeric[2]) return { kind: 'long', label: 0, series: 1, value: 2 }
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
  return roles(cols, numericColumns(t, cols.length)).kind !== 'single'
}

function singlePoints(t: ResultTable, cols: string[]): MultiSeries {
  const labelIdx = indexOfName(cols, 'label', 'name')
  const valueIdx = indexOfName(cols, 'value', 'total')
  const li = labelIdx >= 0 ? labelIdx : 0
  const vi = valueIdx >= 0 ? valueIdx : cols.length > 1 ? 1 : 0
  return {
    shape: 'single',
    labels: t.rows.map((r) => String(r[li] ?? '')),
    series: [{ name: cols[vi] ?? 'value', values: t.rows.map((r) => cell(r[vi]) ?? 0) }],
    folded: [],
  }
}

// Grid collects (label, series) -> value, labels and series in first-seen
// order. A repeated pair is summed: it is the same measure listed twice, as a
// result with no GROUP BY returns it.
class Grid {
  labels: string[] = []
  names: string[] = []
  private li = new Map<string, number>()
  private si = new Map<string, number>()
  private at = new Map<string, number>()

  label(l: string): number {
    let i = this.li.get(l)
    if (i === undefined) this.li.set(l, (i = this.labels.push(l) - 1))
    return i
  }
  name(s: string): number {
    let i = this.si.get(s)
    if (i === undefined) this.si.set(s, (i = this.names.push(s) - 1))
    return i
  }
  add(l: number, s: number, v: number | null) {
    if (v === null) return
    const k = `${l}:${s}`
    this.at.set(k, (this.at.get(k) ?? 0) + v)
  }
  lines(): SeriesLine[] {
    return this.names.map((name, s) => ({
      name,
      values: this.labels.map((_, l) => this.at.get(`${l}:${s}`) ?? null),
    }))
  }
}

// foldSeries keeps the five largest series (by total magnitude, in their own
// order) and sums the rest, and any series already named "Other", into one
// "Other" per label (a gap only where every folded series has one). Up to six
// series are returned as they are, even one named "Other".
export function foldSeries(series: SeriesLine[]): { series: SeriesLine[]; folded: string[] } {
  if (series.length <= MAX_SERIES) return { series, folded: [] }
  const total = (s: SeriesLine) => s.values.reduce<number>((a, v) => a + Math.abs(v ?? 0), 0)
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

function finish(shape: MultiSeries['shape'], g: Grid): MultiSeries {
  const f = foldSeries(g.lines())
  return { shape, labels: g.labels, series: f.series, folded: f.folded }
}

// toMultiSeries shapes a result (or inline columns + rows) into labels and series.
export function toMultiSeries(t: ResultTable): MultiSeries {
  const cols = columnNames(t)
  if (t.rows.length === 0 || cols.length === 0) return EMPTY
  const r = roles(cols, numericColumns(t, cols.length))
  if (r.kind === 'single') return singlePoints(t, cols)
  const g = new Grid()
  if (r.kind === 'long') {
    for (const row of t.rows) {
      const name = String(row[r.series] ?? '')
      g.add(g.label(String(row[r.label] ?? '')), g.name(name === '' ? '(blank)' : name), cell(row[r.value]))
    }
    return finish('long', g)
  }
  const idx = r.values.map((c) => g.name(cols[c]))
  for (const row of t.rows) {
    const l = g.label(String(row[r.label] ?? ''))
    r.values.forEach((c, k) => g.add(l, idx[k], cell(row[c])))
  }
  return finish('wide', g)
}

export interface InlineSeries {
  name: string
  points: { label: string; value: number }[]
}

// fromInlineSeries shapes a lineChart's `series: [{name, points}]`: the union of
// the series' labels, in first-seen order, with a gap where a series lacks one.
export function fromInlineSeries(list: InlineSeries[]): MultiSeries {
  const g = new Grid()
  for (const s of list) {
    const si = g.name(s.name)
    for (const p of s.points ?? []) g.add(g.label(String(p.label ?? '')), si, cell(p.value))
  }
  return finish('inline', g)
}

// fromPoints is the single-series inline form (`points`, `bars`).
export function fromPoints(points: { label: string; value: number }[]): MultiSeries {
  return {
    shape: 'single',
    labels: points.map((p) => String(p.label ?? '')),
    series: [{ name: 'value', values: points.map((p) => cell(p.value) ?? 0) }],
    folded: [],
  }
}
