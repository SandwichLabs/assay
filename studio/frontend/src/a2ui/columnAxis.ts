// columnAxis — the x axis of a columnChart. Columns are bands, not points, so a
// dated chart keeps its evenly spaced columns and only changes what the labels
// say: when every column label is a date, in strictly increasing order (and the
// columns are not sorted by size), the axis shows the time-granularity labels of
// scale.ts ("Apr 3", "Apr '25", "Q2 '25", "2025") under the column each tick
// date falls in, thinned so they never overlap. Anything else is a category
// axis and every column keeps its own label, as before.
//
// The same column-for-a-date rule places event markers (annotations.ts).
import { maxTicksForWidth, parseTime, timeAxis, timeTicks } from './scale'

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

export interface ColumnAxis {
  time: boolean
  // per column: the tick label shown under it, or '' (time axis only)
  labels: string[]
}

const CHAR_PX = 6.6
const PAD_PX = 12

// buildColumnAxis: `centers` are the columns' centres in px from the left of the
// scrolling content, `width` its width (either may be empty/0 before the first
// measure, then the columns are assumed evenly spread over their minimum 24px, or `fallbackW`).
export function buildColumnAxis(labels: string[], ordered: boolean, centers: number[], width: number, fallbackW = 320): ColumnAxis {
  const n = labels.length
  const times = columnTimes(labels, ordered)
  const ta = times && timeAxis(labels)
  if (!times || !ta) return { time: false, labels }
  const w = width > 0 ? width : Math.max(fallbackW, n * 24)
  const cx = centers.length === n ? centers : labels.map((_, i) => ((i + 0.5) / n) * w)
  const out = labels.map(() => '')
  let lastEnd = -Infinity
  let lastCol = -1
  for (const tk of timeTicks(ta.min, ta.max, maxTicksForWidth(w), ta.floor)) {
    const ci = columnAt(times, tk.t)
    if (ci === lastCol) continue
    const half = (tk.label.length * CHAR_PX) / 2
    // never run past the ends of the scrolling content, never touch the last label
    if (cx[ci] - half < 0 || cx[ci] + half > w || cx[ci] - half < lastEnd + PAD_PX) continue
    out[ci] = tk.label
    lastEnd = cx[ci] + half
    lastCol = ci
  }
  return { time: true, labels: out }
}
