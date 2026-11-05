// useAnnotations — a chart's event markers (a2ui/annotations.ts) wired to its
// component: the markers come from the inline `annotations.rows`, from the
// annotations' own live `sql` (+ `connection`), or, inside a data artifact view,
// from a manifest `query`. The second binding goes through useLiveQuery like any
// data-bound field, so it takes the same read-only SQL policy and the shared
// query pool; a failure is a note under the chart, never a missing chart.
//
// `place` is the chart's own rule for where a marker sits on its x axis;
// `widthPx` is the plot's width in px (for the label layout).
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import type { A2UIComponentT } from './types'
import { useLiveQuery } from './useLiveQuery'
import {
  LANE_PX, ariaText, describe, fromResult, fromRows, layoutMarkers, noteText,
  type Annotation, type ParsedAnnotations, type Placement,
} from './annotations'

const NONE: ParsedAnnotations = { items: [], over: 0, skipped: 0, unknown: 0, clipped: 0 }

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
      clipped: parsed.value.clipped,
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

export interface ColumnGeometry {
  // x of each column's centre, px from the left of the scrollable content
  centers: number[]
  // the scrollable content's width and how much of it shows
  width: number
  view: number
  // height the horizontal scrollbar takes from the columns (0 when there is none)
  sbh: number
}

// columnGeometry reads where the columns of a scrolling `.a2ui-columns` grid
// really are (they are at least 20px wide and scroll sideways when there are
// many), so a marker follows its column rather than a fraction of the visible width.
function columnGeometry(el: Pick<HTMLElement, 'scrollWidth' | 'clientWidth' | 'offsetHeight' | 'clientHeight' | 'querySelectorAll'>): ColumnGeometry {
  const cols = Array.from(el.querySelectorAll<HTMLElement>(':scope > .a2ui-columns__col'))
  return {
    centers: cols.map((c) => c.offsetLeft + c.offsetWidth / 2),
    width: el.scrollWidth,
    view: el.clientWidth,
    sbh: Math.max(0, el.offsetHeight - el.clientHeight),
  }
}

// useColumnGeometry: the geometry of the element bound to `el`. `geom` is measured
// when the element appears, on resize and when the caller says the columns changed
// (`measure`); a scroll only updates `scrollLeft` (at most once a frame), so it
// never re-lays-out the markers.
export function useColumnGeometry() {
  const el = ref<HTMLElement | null>(null)
  const geom = ref<ColumnGeometry>({ centers: [], width: 0, view: 0, sbh: 0 })
  const scrollLeft = ref(0)
  let ro: ResizeObserver | undefined
  let frame = 0
  const measure = () => {
    if (!el.value) return
    geom.value = columnGeometry(el.value)
    scrollLeft.value = el.value.scrollLeft
  }
  const onScroll = () => {
    if (frame) return
    const run = () => {
      frame = 0
      if (el.value) scrollLeft.value = el.value.scrollLeft
    }
    frame = typeof requestAnimationFrame === 'function' ? requestAnimationFrame(run) : (run(), 0)
  }
  // the element can appear after mount (a sql-bound chart renders it once its rows arrive)
  watch(el, (e) => {
    ro?.disconnect()
    ro = undefined
    if (!e) return
    measure()
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(measure)
      ro.observe(e)
    }
  }, { flush: 'post' })
  onBeforeUnmount(() => {
    ro?.disconnect()
    if (frame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(frame)
  })
  return { el, geom, scrollLeft, measure, onScroll }
}
