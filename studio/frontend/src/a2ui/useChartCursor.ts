// useChartCursor — the hover/keyboard cursor of a line or area chart: which
// point (by left-to-right position) is being read. The mouse picks the nearest
// point to the pointer; the arrow keys, Home and End step along the points when
// the plot has focus, so values are reachable without a pointer.
import { ref } from 'vue'

export interface PlotGeom {
  // the viewBox width and the plot's left/right padding inside it
  W: number
  padL: number
  innerW: number
}

// nearest: index into ascending `fracs` closest to f.
export function nearest(fracs: number[], f: number): number {
  let best = 0
  for (let i = 1; i < fracs.length; i++) {
    if (Math.abs(fracs[i] - f) < Math.abs(fracs[best] - f)) best = i
  }
  return best
}

export function useChartCursor(fracs: () => number[], geom: PlotGeom) {
  const active = ref<number | null>(null)

  function onMove(e: MouseEvent) {
    const el = e.currentTarget as HTMLElement | null
    const f = fracs()
    if (!el || f.length === 0) return
    const r = el.getBoundingClientRect()
    if (r.width <= 0) return
    const inner = ((e.clientX - r.left) / r.width * geom.W - geom.padL) / geom.innerW
    active.value = nearest(f, inner)
  }

  function onKey(e: KeyboardEvent) {
    const n = fracs().length
    if (n === 0) return
    const cur = active.value ?? (e.key === 'ArrowLeft' ? n : -1)
    let next: number | null = null
    if (e.key === 'ArrowRight') next = Math.min(n - 1, cur + 1)
    else if (e.key === 'ArrowLeft') next = Math.max(0, cur - 1)
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = n - 1
    else if (e.key === 'Escape') {
      active.value = null
      return
    }
    if (next === null) return
    e.preventDefault()
    active.value = next
  }

  function onFocus() {
    if (active.value === null && fracs().length) active.value = fracs().length - 1
  }

  function clear() {
    active.value = null
  }

  // The pointer leaving does not end a keyboard reading: while the plot has
  // focus the cursor stays where the arrow keys put it.
  function onLeave(e: MouseEvent) {
    const el = e.currentTarget as HTMLElement | null
    if (el && typeof document !== 'undefined' && el.contains(document.activeElement)) return
    active.value = null
  }

  return { active, onMove, onKey, onFocus, onLeave, clear }
}
