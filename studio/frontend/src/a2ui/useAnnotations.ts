// useAnnotations — a chart's event markers (a2ui/annotations.ts) wired to its
// component: the markers come from the inline `annotations.rows`, from the
// annotations' own live `sql` (+ `connection`), or, inside a data artifact view,
// from a manifest `query`. The second binding goes through useLiveQuery like any
// data-bound field, so it takes the same read-only SQL policy and the shared
// query pool; a failure is a note under the chart, never a missing chart.
//
// `place` is the chart's own rule for where a marker sits on its x axis;
// `widthPx` is the plot's width in px (for the label layout).
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { A2UIComponentT } from './types'
import { useLiveQuery } from './useLiveQuery'
import {
  LANE_PX, ariaText, describe, fromResult, fromRows, layoutMarkers, noteText,
  type Annotation, type ParsedAnnotations, type Placement,
} from './annotations'

const NONE: ParsedAnnotations = { items: [], over: 0, skipped: 0, unknown: 0 }

export function useAnnotations(
  component: () => A2UIComponentT,
  place: (items: Annotation[]) => Placement,
  widthPx: () => number,
) {
  const spec = () => component().annotations as { rows?: unknown; sql?: string; connection?: string; query?: string } | undefined
  const live = useLiveQuery(() => ({ id: 'annotations', type: 'annotations', sql: spec()?.sql, connection: spec()?.connection, query: spec()?.query }))

  const parsed = computed<ParsedAnnotations>(() => {
    const s = spec()
    if (!s) return NONE
    if (Array.isArray(s.rows)) return fromRows(s.rows)
    return live.data.value ? fromResult(live.data.value) : NONE
  })
  const placement = computed(() => place(parsed.value.items))
  const layout = computed(() => layoutMarkers(placement.value.markers, widthPx()))

  const note = computed(() =>
    noteText({
      over: parsed.value.over,
      skipped: parsed.value.skipped,
      unknown: parsed.value.unknown,
      outside: placement.value.outside,
      hidden: layout.value.hidden,
      error: live.error.value,
    }),
  )
  // headroom above the plot for the label rows
  const wrapStyle = computed(() => (layout.value.lanes ? { paddingTop: layout.value.lanes * LANE_PX + 'px' } : {}))
  // the markers that belong to chart label i (the point they fall at or after)
  const notesAt = (i: number | null): string[] => (i === null ? [] : layout.value.markers.filter((m) => m.bucket === i).map(describe))

  return {
    markers: computed(() => layout.value.markers),
    note,
    wrapStyle,
    notesAt,
    ariaText: computed(() => ariaText(placement.value.markers)),
  }
}

// useElementWidth: the live width in px of the element bound to `el`.
export function useElementWidth() {
  const el = ref<HTMLElement | null>(null)
  const width = ref(0)
  let ro: ResizeObserver | undefined
  const measure = () => (width.value = el.value?.clientWidth || 0)
  onMounted(() => {
    measure()
    if (typeof ResizeObserver !== 'undefined' && el.value) {
      ro = new ResizeObserver(measure)
      ro.observe(el.value)
    }
  })
  onBeforeUnmount(() => ro?.disconnect())
  return { el, width }
}
