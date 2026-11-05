// xaxis — where a chart's labels sit along x, and which of them get a tick
// label. Dates and timestamps get a real time axis (position by time, ticks on
// calendar boundaries); anything else is evenly spaced categories, thinned to
// what fits (query.pickLabelIndices). Shared by the line and area charts.
import { pickLabelIndices } from './query'
import { maxTicksForWidth, timeAxis, timeFrac, timeTicks, type TimeAxis } from './scale'

export interface XTick {
  frac: number
  label: string
}

export interface XAxis {
  time: boolean
  // the time scale behind `frac` (null for categories), for placing a date between labels
  axis: TimeAxis | null
  // x position (0..1) of each input label, in input order
  frac: number[]
  // input indices sorted left to right (input order for categories)
  order: number[]
  ticks: XTick[]
}

export function buildXAxis(labels: string[], widthPx: number, viewW: number): XAxis {
  const n = labels.length
  const ta = timeAxis(labels)
  if (ta) {
    const order = labels.map((_, i) => i).sort((a, b) => ta.times[a] - ta.times[b] || a - b)
    const ticks = timeTicks(ta.min, ta.max, maxTicksForWidth(widthPx), ta.floor).map((t) => ({ frac: timeFrac(ta, t.t), label: t.label }))
    return { time: true, axis: ta, frac: ta.times.map((t) => timeFrac(ta, t)), order, ticks }
  }
  const frac = labels.map((_, i) => (n === 1 ? 0.5 : i / (n - 1)))
  const ticks = pickLabelIndices(labels, widthPx, viewW).map((i) => ({ frac: frac[i], label: labels[i] }))
  return { time: false, axis: null, frac, order: labels.map((_, i) => i), ticks }
}

// tickAnchor: how an x tick label sits on its position, so the first and last
// never hang past the card's edge.
export function tickAnchor(frac: number): Record<string, string> {
  const left = `${(frac * 100).toFixed(3)}%`
  if (frac <= 0.08) return { left, transform: 'translateX(0)' }
  if (frac >= 0.92) return { left, transform: 'translateX(-100%)' }
  return { left, transform: 'translateX(-50%)' }
}
