<script setup lang="ts">
// A2UILineChart — an SVG line chart: inline "points", "series" or "columns" +
// "rows", or a live "sql" binding. role="img" + aria-label (a chart with no
// text alternative is inaccessible).
//
// Several series (a2ui/multiseries.ts): a long (label, series, value) or wide
// (label, n1, n2, ...) result, or inline `series`. They share one value scale
// and one x axis (the union of the labels); a series with no point at a label
// has a gap there, never a zero. Colours come from the theme's chart roles by
// series index, six at most, the rest summed into "Other".
//
// Axes (a2ui/scale.ts, a2ui/xaxis.ts): round gridlines (1/2/5 x 10^n, usually four or five), a
// zero baseline for non-negative data unless the component says `zero: false`,
// and a real time axis when every label is a date or timestamp: points sit by
// time, not by index, with ticks on calendar boundaries ("Apr '25").
// Reading values: hover the plot, or focus it and use the arrow keys / Home /
// End; the label and every series' value show in a tooltip and in a live region.
//
// Axis labels render as HTML (A2UIPlotFrame), not SVG <text>: an SVG font size
// is in viewBox units, which scale down with the SVG, so at the agent panel's
// ~410px width a 9-unit label is ~5 real pixels. The x tick set is decided from
// the plot's measured pixel width.
import { ref, computed } from 'vue'
import type { A2UIComponentT } from './types'
import { colorClass, isBound } from './query'
import { useMultiSeries } from './useMultiSeries'
import { formatNumber } from '../lib/formatCell'
import { useMotion } from './useMotion'
import { valueScale, frac as scaleFrac, formatTick } from './scale'
import { buildXAxis } from './xaxis'
import { seriesClass } from './series'
import { useChartCursor } from './useChartCursor'
import A2UIPlotFrame from './A2UIPlotFrame.vue'
import A2UISeriesLegend from './A2UISeriesLegend.vue'
import A2UISeriesTip from './A2UISeriesTip.vue'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, ms } = useMultiSeries(() => props.component, 'points')
const multi = computed(() => ms.value.series.length > 1)
const labels = computed(() => ms.value.labels)
const n = computed(() => labels.value.length)

const W = 640
const H = 200
const PAD = { top: 10, right: 12, bottom: 10, left: 12 }
const innerW = W - PAD.left - PAD.right
const innerH = H - PAD.top - PAD.bottom

const values = computed(() => ms.value.series.flatMap((s) => s.values.filter((v): v is number => v !== null)))
const scale = computed(() => valueScale(values.value, { zero: props.component.zero !== false }))
const fmtTick = (v: number) => formatTick(v, scale.value.step)

// The lines rise from the bottom gridline with the component's "motion"
// preset; axis labels and tooltips always show the real values.
const { values: anim } = useMotion(
  () => ms.value.series.flatMap((s) => s.values.map((v) => (v ?? scale.value.min) - scale.value.min)),
  () => props.component.motion,
)

const plotWidthPx = ref(0)
const xa = computed(() => buildXAxis(labels.value, plotWidthPx.value, W))

function x(i: number): number {
  return PAD.left + xa.value.frac[i] * innerW
}
function y(v: number): number {
  return PAD.top + innerH - scaleFrac(scale.value, v) * innerH
}
// the drawn y of series si at label i (0 where the series has no point there)
function py(si: number, i: number): number {
  const v = ms.value.series[si].values[i]
  return v === null ? 0 : y((anim.value[si * n.value + i] ?? v - scale.value.min) + scale.value.min)
}

// Each series as runs of consecutive points, left to right: a missing point ends the run.
const runs = computed(() =>
  ms.value.series.map((s) => {
    const out: number[][] = []
    let cur: number[] = []
    for (const i of xa.value.order) {
      if (s.values[i] !== null) cur.push(i)
      else if (cur.length) { out.push(cur); cur = [] }
    }
    if (cur.length) out.push(cur)
    return out
  }),
)
const poly = (si: number, run: number[]) => run.map((i) => `${x(i)},${py(si, i)}`).join(' ')
const dotsOf = (si: number) => labels.value.map((_, i) => i).filter((i) => ms.value.series[si].values[i] !== null)

const yTicks = computed(() => scale.value.ticks.map((t) => ({ pct: (y(t) / H) * 100, label: fmtTick(t) })))
const xTicks = computed(() => xa.value.ticks.map((t) => ({ frac: (PAD.left + t.frac * innerW) / W, label: t.label })))
const axw = computed(() => Math.max(2, ...scale.value.ticks.map((t) => fmtTick(t).length)) + 1)

// Cursor: positions run left to right (xa.order); `activeIdx` maps back to the data.
const orderedFracs = computed(() => xa.value.order.map((i) => xa.value.frac[i]))
const cursor = useChartCursor(() => orderedFracs.value, { W, padL: PAD.left, innerW })
const activeIdx = computed(() => (cursor.active.value === null ? null : (xa.value.order[cursor.active.value] ?? null)))
// every series that has a point at the cursor's label
const readout = computed(() => {
  const i = activeIdx.value
  if (i === null) return []
  return ms.value.series.flatMap((s, si) => (s.values[i] === null ? [] : [{ si, name: s.name, value: formatNumber(s.values[i] as number) }]))
})
const activeText = computed(() => {
  const i = activeIdx.value
  if (i === null) return ''
  return `${labels.value[i]}: ${readout.value.map((r) => (multi.value ? `${r.name} ${r.value}` : r.value)).join(', ')}`
})
const tipStyle = computed<{ left?: string; top?: string }>(() => {
  const i = activeIdx.value
  if (i === null) return {}
  const top = Math.min(...readout.value.map((r) => py(r.si, i)))
  return { left: (x(i) / W) * 100 + '%', top: (top / H) * 100 + '%' }
})
const tipSide = computed(() => (activeIdx.value !== null && xa.value.frac[activeIdx.value] > 0.6 ? 'left' : 'right'))

const showDots = computed(() => n.value <= 60)
const ariaLabel = computed(() => {
  const v = values.value
  const span = xa.value.time && xa.value.ticks.length ? `, ${labels.value[xa.value.order[0]]} to ${labels.value[xa.value.order[xa.value.order.length - 1]]}` : ''
  const what = multi.value ? `${ms.value.series.length} series (${ms.value.series.map((s) => s.name).join(', ')}) over ${n.value} labels` : `${v.length} points`
  return `Line chart of ${what}${span}, ranging ${formatNumber(Math.min(...v))} to ${formatNumber(Math.max(...v))}`
})
</script>

<template>
  <div class="a2ui-linechart">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="isBound(component) && loading" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="isBound(component) && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="n === 0 || ms.series.length === 0" class="a2ui-empty">NO ROWS</div>
    <template v-else>
      <A2UIPlotFrame
        data-testid="a2ui-linechart-plot" :y-ticks="yTicks" :x-ticks="xTicks" :axw="axw"
        tabindex="0" role="group" aria-label="Chart values. Use the left and right arrow keys to read each point."
        @measure="plotWidthPx = $event"
        @mousemove="cursor.onMove" @mouseleave="cursor.onLeave" @keydown="cursor.onKey" @focus="cursor.onFocus" @blur="cursor.clear"
      >
        <svg :viewBox="`0 0 ${W} ${H}`" :class="['a2ui-linechart__svg', multi ? '' : colorClass(component.color)]" role="img" :aria-label="ariaLabel">
          <line
            v-for="(t, i) in scale.ticks" :key="'g' + i" :x1="PAD.left" :x2="W - PAD.right" :y1="y(t)" :y2="y(t)"
            class="a2ui-linechart__grid" :class="{ 'a2ui-linechart__grid--zero': t === 0 && scale.min < 0 }"
          />
          <line v-if="activeIdx !== null" :x1="x(activeIdx)" :x2="x(activeIdx)" :y1="PAD.top" :y2="H - PAD.bottom" class="a2ui-linechart__cursor" />
          <g v-for="(s, si) in ms.series" :key="si" :class="multi ? ['a2ui-linechart__series', seriesClass(si)] : undefined">
            <template v-for="(run, ri) in runs[si]" :key="ri">
              <polyline v-if="run.length > 1" class="a2ui-linechart__line" :points="poly(si, run)" />
            </template>
            <template v-if="showDots">
              <circle v-for="i in dotsOf(si)" :key="'dot' + i" :cx="x(i)" :cy="py(si, i)" r="3.5" class="a2ui-linechart__dot">
                <title>{{ labels[i] }}: {{ multi ? s.name + ' ' : '' }}{{ formatNumber(s.values[i] as number) }}</title>
              </circle>
            </template>
            <circle v-if="activeIdx !== null && s.values[activeIdx] !== null" :cx="x(activeIdx)" :cy="py(si, activeIdx)" r="5.5" class="a2ui-linechart__focus" />
          </g>
        </svg>
        <A2UISeriesTip v-if="activeIdx !== null" :label="labels[activeIdx]" :rows="readout" :multi="multi" :side="tipSide" :pos="tipStyle" />
        <span class="a2ui-sr" aria-live="polite">{{ activeText }}</span>
      </A2UIPlotFrame>
      <A2UISeriesLegend :names="ms.series.map((s) => s.name)" :folded="ms.folded" />
    </template>
  </div>
</template>
