// useMultiSeries — the data of a lineChart or barChart as labels and series
// (a2ui/multiseries.ts), from wherever the component keeps it: a live sql
// binding or a manifest query, inline columns + rows (what a notebook cell
// hands over), an inline `series` list, or the single-series `points` / `bars`.
import { computed } from 'vue'
import type { A2UIComponentT } from './types'
import { isBound } from './query'
import { useLiveQuery } from './useLiveQuery'
import { fromInlineSeries, fromPoints, toMultiSeries, type MultiSeries } from './multiseries'

export function useMultiSeries(component: () => A2UIComponentT, field: 'points' | 'bars') {
  // lines drop the series past six, bars sum them into Other (multiseries.foldSeries)
  const mode = field === 'points' ? 'drop' : 'sum'
  const { loading, error, data } = useLiveQuery(component)
  const ms = computed<MultiSeries>(() => {
    const c = component()
    if (isBound(c)) return toMultiSeries(data.value ?? { columns: [], rows: [] }, mode)
    if (Array.isArray(c.rows)) return toMultiSeries({ columns: c.columns ?? [], rows: c.rows, types: c.types }, mode)
    if (Array.isArray(c.series)) return fromInlineSeries(c.series, mode)
    return fromPoints(c[field] ?? [])
  })
  return { loading, error, data, ms }
}
