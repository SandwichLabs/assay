// columnAxis — the x axis of a columnChart. Columns are bands, not points, so a
// dated chart keeps its evenly spaced columns and only changes what the labels
// say: when every column label is a date, in strictly increasing order (and the
// columns are not sorted by size), the axis shows the time-granularity labels of
// scale.ts ("Apr 3", "Apr '25", "Q2 '25", "2025") under the column each tick
// date falls in, thinned so they never overlap. Anything else is a category
// axis and every column keeps its own label, as before.
//
// The same column-for-a-date rule places event markers (annotations.ts).
import { parseTime, timeAxis, timePlan } from './scale'

// columnTimes: each column's time, or null unless the columns are dates in
// strictly increasing order (`ordered` false means a size sort: never dated).
export function columnTimes(labels: string[], ordered: boolean): number[] | null {
  if (!ordered || labels.length < 2) return null
  const times: number[] = []
  for (const l of labels) {
    const t = parseTime(l)
    if (t === null || (times.length > 0 && t <= times[times.length - 1])) return null
    times.push(t)
  }
  return times
}

// columnAt: the column a time falls in, the last one that starts at or before it
// (the first column when it is earlier than all of them).
export function columnAt(times: number[], t: number): number {
  let i = 0
  while (i + 1 < times.length && times[i + 1] <= t) i++
  return i
}

// How a label sits on its column: centred, or (the first and last column only)
// left- or right-aligned so it never hangs past the ends of the content.
type Anchor = '' | 'start' | 'end'

export interface ColumnAxis {
  time: boolean
  // per column: the label shown under it, or '' (time axis only)
  labels: string[]
  anchors: Anchor[]
}

const CHAR_PX = 6.6
const PAD_PX = 12
const SLOT_PX = 94 // a label and its breathing room, for sizing the tick budget

interface Slot {
  ci: number
  label: string
  anchor: Anchor
  left: number
  right: number
}

// buildColumnAxis: `centers` are the columns' centres in px from the left of the
// scrolling content, `width` its width (either may be empty/0 before the first
// measure: the columns are then assumed evenly spread over at least 24px each).
//
// Tick dates come from the same calendar steps as the line chart, but never a
// step finer than the closest pair of columns, and a tick is only used on a
// column whose own span [time, next time) holds it. The label is always the
// column's own time at the step's granularity, so it can never name a date the
// column does not hold. Fewer than two labels (a short or narrow chart) falls
// back to columns spread evenly by index, each labelled with its own date.
export function buildColumnAxis(labels: string[], ordered: boolean, centers: number[], width: number, fallbackW = 320): ColumnAxis {
  const n = labels.length
  const times = columnTimes(labels, ordered)
  const ta = times && timeAxis(labels)
  if (!times || !ta) return { time: false, labels, anchors: labels.map(() => '') }
  const w = width > 0 ? width : Math.max(fallbackW, n * 24)
  const cx = centers.length === n ? centers : labels.map((_, i) => ((i + 0.5) / n) * w)
  const cw = w / n
  const gap = (i: number) => (i < n - 1 ? times[i + 1] - times[i] : times[i] - times[i - 1])
  const minGap = Math.min(...times.slice(1).map((t, i) => t - times[i]))

  const fit = (ci: number, label: string): Slot | null => {
    const lw = label.length * CHAR_PX
    let left = cx[ci] - lw / 2
    let anchor: Anchor = ''
    if (left < 0) {
      if (ci !== 0) return null
      left = Math.max(0, cx[ci] - cw / 2)
      anchor = 'start'
    } else if (left + lw > w) {
      if (ci !== n - 1) return null
      left = Math.min(w, cx[ci] + cw / 2) - lw
      anchor = 'end'
    }
    return left < 0 || left + lw > w + 0.5 ? null : { ci, label, anchor, left, right: left + lw }
  }
  // greedy left to right: keep a slot only when it clears the last kept one
  const layout = (cands: Array<[number, string]>): Slot[] => {
    const out: Slot[] = []
    for (const [ci, label] of cands) {
      const s = fit(ci, label)
      const last = out[out.length - 1]
      if (s && (!last || (s.left >= last.right + PAD_PX && s.label !== last.label))) out.push(s)
    }
    return out
  }

  const budget = Math.min(120, Math.max(3, Math.floor(w / SLOT_PX)))
  const plan = timePlan(ta.min, ta.max, budget, ta.floor, minGap)
  const cands: Array<[number, string]> = []
  // A bucketed step (month, quarter, year) labels the bucket: a tick goes on the
  // column whose own bucket it is, the next column when the tick falls in a
  // column of the previous bucket (a week starting Jan 26 is not "Feb").
  for (const t of plan.exact ? plan.ticks : []) {
    let ci = columnAt(times, t)
    if (plan.bucketed && plan.format(times[ci]) !== plan.format(t) && ci + 1 < n && plan.format(times[ci + 1]) === plan.format(t)) ci++
    const ok = plan.bucketed ? plan.format(times[ci]) === plan.format(t) : t >= times[ci] && t < times[ci] + gap(ci)
    if (ok && !cands.some(([c]) => c === ci)) cands.push([ci, plan.format(times[ci])])
  }
  let slots = layout(cands)
  // the first and last column are labelled whenever they fit
  const own = (ci: number): Slot | null => (plan.exact ? fit(ci, plan.format(times[ci])) : null)
  const first = own(0)
  if (first && (!slots.length || (slots[0].ci !== 0 && first.label !== slots[0].label && first.right + PAD_PX <= slots[0].left))) slots = [first, ...slots]
  const lastS = own(n - 1)
  const tail = slots[slots.length - 1]
  if (lastS && (!tail || (tail.ci !== n - 1 && lastS.label !== tail.label && lastS.left >= tail.right + PAD_PX))) slots = [...slots, lastS]

  if (slots.length < 2) {
    // few columns or a narrow chart: spread labels evenly by index, as many as fit
    const fmt = timePlan(ta.min, ta.max, Math.max(2, n), ta.floor, minGap).format
    const all = (k: number) => Array.from({ length: k }, (_, j): [number, string] => {
      const ci = Math.round((j * (n - 1)) / (k - 1))
      return [ci, fmt(times[ci])]
    })
    for (let k = Math.min(n, Math.floor(w / SLOT_PX) + 1); k >= 2; k--) {
      const s = layout(all(k))
      if (s.length === k) {
        slots = s
        break
      }
    }
  }
  const out = labels.map(() => '')
  const anchors = labels.map((): Anchor => '')
  for (const s of slots) {
    out[s.ci] = s.label
    anchors[s.ci] = s.anchor
  }
  return { time: true, labels: out, anchors }
}
