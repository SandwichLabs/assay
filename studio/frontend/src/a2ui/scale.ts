// scale — the shared axis math for the A2UI charts: nice value ticks, the
// zero-baseline rule, and a real time axis. Pure (no Vue, no DOM) so the line,
// area, column and bar charts share one definition of "where does a value
// go", and so a later overlay (annotations, #320) can place a marker with the
// very same scale the chart drew its gridlines with.
//
// Time is wall-clock: a label such as "2025-03-09 02:30" is read as that
// calendar time in UTC arithmetic, ignoring any zone suffix, so DST never
// shifts a tick and the same data reads the same in every locale.

export interface NiceScale {
  // tick values, ascending; ticks[0] === min and the last tick === max
  ticks: number[]
  min: number
  max: number
  step: number
}

const STEP_MANTISSAS = [1, 2, 5]
const TARGET_TICKS = 5

function decimalsFor(step: number): number {
  if (!(step > 0) || !Number.isFinite(step)) return 0
  return Math.min(10, Math.max(0, -Math.floor(Math.log10(step) + 1e-9)))
}

// tickDigits: fraction digits a tick label needs for this step (0.005 -> 3),
// for formatNumber's second argument.
export function tickDigits(step: number): number {
  return Math.min(8, Math.max(2, decimalsFor(step)))
}

function tidy(v: number, decimals: number): number {
  const r = Number(v.toFixed(decimals))
  return Object.is(r, -0) ? 0 : r
}

// niceTicks returns round gridline values (steps of 1, 2 or 5 times a power of
// ten) covering [lo, hi]. It prefers 4 or 5 ticks, and settles for 3 to 6 when
// no round step gives 4 or 5. The returned min/max are the outermost ticks,
// so the plot's value domain is exactly the gridline range.
export function niceTicks(lo: number, hi: number): NiceScale {
  if (!Number.isFinite(lo) || !Number.isFinite(hi)) return { ticks: [0, 1], min: 0, max: 1, step: 1 }
  if (lo > hi) [lo, hi] = [hi, lo]
  const mag = Math.max(Math.abs(lo), Math.abs(hi))
  if (hi - lo <= mag * 1e-9) {
    // A single value (or a range lost in rounding) has no range of its own:
    // open one around it, so no step is finer than the numbers can carry.
    const pad = mag === 0 ? 1 : mag * 0.1
    lo -= pad
    hi += pad
  }
  const range = hi - lo
  // An overflowed (Infinity) or underflowed (0) range would never find a step.
  if (!(range > 0) || !Number.isFinite(range)) return { ticks: [0, 1], min: 0, max: 1, step: 1 }
  const e0 = Math.floor(Math.log10(range))
  let best: { step: number; a: number; b: number; cost: number; width: number } | null = null
  for (let e = e0 - 2; e <= e0 + 1; e++) {
    for (const m of STEP_MANTISSAS) {
      const step = m * 10 ** e
      if (!(step > 0) || !Number.isFinite(step)) continue
      const a = Math.floor(lo / step + 1e-9)
      const b = Math.ceil(hi / step - 1e-9)
      const count = b - a + 1
      if (!(count >= 3 && count <= 6)) continue
      // 4 or 5 ticks cost nothing; 3 or 6 cost a little; then the tightest fit.
      const cost = count === 4 || count === 5 ? 0 : 1
      const width = (b - a) * step
      if (!best || cost < best.cost || (cost === best.cost && width < best.width - 1e-12)) best = { step, a, b, cost, width }
    }
  }
  if (!best) {
    // Unreachable for finite ranges (some step always yields 3 to 6 ticks),
    // but keep a defined answer.
    return { ticks: [0, 1], min: 0, max: 1, step: 1 }
  }
  const dec = decimalsFor(best.step) + 1
  const ticks: number[] = []
  for (let k = best.a; k <= best.b; k++) ticks.push(tidy(k * best.step, dec))
  return { ticks, min: ticks[0], max: ticks[ticks.length - 1], step: best.step }
}

// valueScale picks the value axis for a set of plotted numbers. Unless
// `zero === false`, an all-non-negative series starts at 0 and an
// all-non-positive series ends at 0 (a bar or area's length means magnitude,
// and a truncated axis exaggerates differences). Mixed-sign data spans its
// own range, which already contains 0. `zero: false` fits the data instead.
export function valueScale(values: number[], opts: { zero?: boolean } = {}): NiceScale {
  const finite = values.filter((v) => Number.isFinite(v))
  if (finite.length === 0) return niceTicks(0, 1)
  let lo = Math.min(...finite)
  let hi = Math.max(...finite)
  if (opts.zero !== false) {
    if (lo > 0) lo = 0
    if (hi < 0) hi = 0
  }
  if (lo === hi && lo === 0) hi = 1
  return niceTicks(lo, hi)
}

// frac maps a value to 0 (scale.min) .. 1 (scale.max).
export function frac(s: NiceScale, v: number): number {
  const span = s.max - s.min
  return span === 0 ? 0 : (v - s.min) / span
}

// ---- time ----

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const MIN = 60_000
const HOUR = 60 * MIN
const DAY = 24 * HOUR

// "2025-04", "2025-04-03", "2025-04-03 10:30[:15[.5]]" or with "T", with an
// optional zone suffix (read as wall-clock, see the file comment).
const TIME_RE = /^(\d{4})-(\d{2})(?:-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?)?)?(?:Z|[+-]\d{2}(?::?\d{2})?)?$/

// parseTime reads a date/timestamp label as UTC milliseconds, or null when it
// is not one (or names an impossible date).
export function parseTime(label: string): number | null {
  const m = TIME_RE.exec(label.trim())
  if (!m) return null
  const [y, mo, d, h, mi, s] = [m[1], m[2], m[3] ?? '1', m[4] ?? '0', m[5] ?? '0', m[6] ?? '0'].map(Number)
  if (y < 100 || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || s > 59) return null
  const t = Date.UTC(y, mo - 1, d, h, mi, s)
  // Date.UTC rolls 31 Feb into March; a real date round-trips.
  const back = new Date(t)
  if (back.getUTCMonth() !== mo - 1 || back.getUTCDate() !== d) return null
  return t
}

export type TimeUnit = Unit

export interface TimeTick {
  t: number
  label: string
}

type Unit = 'second' | 'minute' | 'hour' | 'day' | 'month' | 'year'
const UNIT_RANK: Record<Unit, number> = { second: 0, minute: 1, hour: 2, day: 3, month: 4, year: 5 }
interface Step {
  unit: Unit
  n: number
  week?: boolean
}

// Finest first; the first step with few enough ticks wins.
const STEPS: Step[] = [
  { unit: 'second', n: 1 }, { unit: 'second', n: 5 }, { unit: 'second', n: 15 }, { unit: 'second', n: 30 },
  { unit: 'minute', n: 1 }, { unit: 'minute', n: 5 }, { unit: 'minute', n: 15 }, { unit: 'minute', n: 30 },
  { unit: 'hour', n: 1 }, { unit: 'hour', n: 2 }, { unit: 'hour', n: 3 }, { unit: 'hour', n: 6 }, { unit: 'hour', n: 12 },
  { unit: 'day', n: 1 }, { unit: 'day', n: 2 }, { unit: 'day', n: 7, week: true }, { unit: 'day', n: 14, week: true },
  { unit: 'month', n: 1 }, { unit: 'month', n: 2 }, { unit: 'month', n: 3 }, { unit: 'month', n: 6 },
  { unit: 'year', n: 1 }, { unit: 'year', n: 2 }, { unit: 'year', n: 5 }, { unit: 'year', n: 10 },
  { unit: 'year', n: 25 }, { unit: 'year', n: 50 }, { unit: 'year', n: 100 },
]

const MAX_TICKS_SCAN = 400

function ticksFor(min: number, max: number, st: Step): number[] {
  const out: number[] = []
  const d0 = new Date(min)
  let t: number
  if (st.unit === 'second' || st.unit === 'minute' || st.unit === 'hour') {
    const unitMs = st.unit === 'second' ? 1000 : st.unit === 'minute' ? MIN : HOUR
    const stepMs = st.n * unitMs
    t = Math.ceil(min / stepMs) * stepMs
    for (; t <= max && out.length < MAX_TICKS_SCAN; t += stepMs) out.push(t)
  } else if (st.unit === 'day') {
    t = Date.UTC(d0.getUTCFullYear(), d0.getUTCMonth(), d0.getUTCDate())
    if (t < min) t += DAY
    if (st.week) {
      // Weeks start on Monday.
      while (new Date(t).getUTCDay() !== 1) t += DAY
    }
    for (; t <= max && out.length < MAX_TICKS_SCAN; t += st.n * DAY) out.push(t)
  } else if (st.unit === 'month') {
    let y = d0.getUTCFullYear()
    let mo = d0.getUTCMonth()
    // Months align to multiples of n from January, so a 3-month step is quarters.
    mo -= mo % st.n
    for (let i = 0; i < MAX_TICKS_SCAN; i++) {
      t = Date.UTC(y, mo, 1)
      if (t > max) break
      if (t >= min) out.push(t)
      mo += st.n
      if (mo >= 12) {
        y += Math.floor(mo / 12)
        mo %= 12
      }
    }
  } else {
    let y = d0.getUTCFullYear()
    y -= ((y % st.n) + st.n) % st.n
    for (let i = 0; i < MAX_TICKS_SCAN; i++) {
      t = Date.UTC(y, 0, 1)
      if (t > max) break
      if (t >= min) out.push(t)
      y += st.n
    }
  }
  return out
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function tickLabel(t: number, st: Step, multiYear: boolean, multiDay: boolean): string {
  const d = new Date(t)
  const yy = `'${pad2(d.getUTCFullYear() % 100)}`
  switch (st.unit) {
    case 'second':
    case 'minute':
    case 'hour': {
      const hm = `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())}${st.unit === 'second' ? `:${pad2(d.getUTCSeconds())}` : ''}`
      return multiDay ? `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()} ${hm}` : hm
    }
    case 'day':
      return `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}${multiYear ? ` ${yy}` : ''}`
    case 'month':
      if (st.n === 3) return `Q${Math.floor(d.getUTCMonth() / 3) + 1} ${yy}`
      return `${MONTHS[d.getUTCMonth()]} ${yy}`
    default:
      return String(d.getUTCFullYear())
  }
}

// timeTicks chooses the finest granularity (minutes up to centuries; days,
// weeks, months, quarters, years in between) that keeps the tick count at or
// under maxTicks, and labels the ticks to match: "Apr 3", "Apr '25", "Q2 '25",
// "2025". Ticks are real calendar boundaries inside [min, max].
export function timeTicks(min: number, max: number, maxTicks = 6, floor: TimeUnit = 'second'): TimeTick[] {
  const cap = Math.max(2, Math.floor(maxTicks))
  const multiYear = new Date(min).getUTCFullYear() !== new Date(max).getUTCFullYear()
  const multiDay = Math.floor(min / DAY) !== Math.floor(max / DAY)
  if (!(max > min) || !Number.isFinite(min) || !Number.isFinite(max)) return []
  for (const st of STEPS) {
    // never finer than the data: a month-resolution label has no weeks in it
    if (UNIT_RANK[st.unit] < UNIT_RANK[floor]) continue
    const ts = ticksFor(min, max, st)
    if (ts.length <= cap) {
      if (ts.length < 2) continue
      return ts.map((t) => ({ t, label: tickLabel(t, st, multiYear, multiDay) }))
    }
  }
  // Nothing fits: fall back to the two ends, labelled to suit the span.
  const end: Step = max - min < DAY ? { unit: 'second', n: 1 } : STEPS[STEPS.length - 1]
  return [min, max].map((t) => ({ t, label: tickLabel(t, end, multiYear, multiDay) }))
}

// timeGranularity: the finest unit a label states, so ticks are never finer
// than the data ("2025-01" has no days; "2025-04-03" has no hours).
export function timeGranularity(label: string): TimeUnit {
  const m = TIME_RE.exec(label.trim())
  if (!m || m[3] === undefined) return 'month'
  if (m[4] === undefined) return 'day'
  return m[6] !== undefined && Number(m[6]) !== 0 ? 'second' : 'minute'
}

export interface TimeAxis {
  // one time per input label, in input order
  times: number[]
  // the finest unit any label states
  floor: TimeUnit
  min: number
  max: number
}

// timeAxis reads every label as a date or timestamp. It returns null unless
// they all parse and at least two differ (one instant has no axis), in which
// case the chart falls back to evenly spaced categories.
export function timeAxis(labels: string[]): TimeAxis | null {
  if (labels.length < 2) return null
  const times: number[] = []
  let floor: TimeUnit = 'year'
  for (const l of labels) {
    const g = timeGranularity(l)
    if (UNIT_RANK[g] < UNIT_RANK[floor]) floor = g
    const t = parseTime(l)
    if (t === null) return null
    times.push(t)
  }
  const min = Math.min(...times)
  const max = Math.max(...times)
  return min === max ? null : { times, floor, min, max }
}

// timeFrac maps a time to 0 (axis.min) .. 1 (axis.max).
export function timeFrac(a: Pick<TimeAxis, 'min' | 'max'>, t: number): number {
  return a.max === a.min ? 0.5 : (t - a.min) / (a.max - a.min)
}

// maxTicksForWidth: how many time-tick labels of about `labelPx` fit across
// `widthPx` (clamped to 3..8). Width 0 (not measured yet) means the default.
export function maxTicksForWidth(widthPx: number, labelPx = 70): number {
  if (!(widthPx > 0)) return 6
  return Math.min(8, Math.max(3, Math.floor(widthPx / (labelPx + 24))))
}
