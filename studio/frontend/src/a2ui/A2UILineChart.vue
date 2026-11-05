<script setup lang="ts">
// A2UILineChart — an SVG line chart, inline "points" or a live "sql" binding.
// role="img" + aria-label (a chart with no text alternative is inaccessible).
//
// Axes (a2ui/scale.ts, a2ui/xaxis.ts): 4-6 round gridlines (1/2/5 x 10^n), a
// zero baseline for non-negative data unless the component says `zero: false`,
// and a real time axis when every label is a date or timestamp: points sit by
// time, not by index, with ticks on calendar boundaries ("Apr '25").
// Reading values: hover the plot, or focus it and use the arrow keys / Home /
// End; the point's label and value show in a tooltip and in a live region.
//
// Axis labels render as HTML (A2UIPlotFrame), not SVG <text>: an SVG font size
// is in viewBox units, which scale down with the SVG, so at the agent panel's
// ~410px width a 9-unit label is ~5 real pixels. The x tick set is decided from
// the plot's measured pixel width.
import { ref, computed } from 'vue'
import type { A2UIComponentT } from './types'
import { toPoints, colorClass, type Point, isBound } from './query'
import { useLiveQuery } from './useLiveQuery'
import { formatNumber } from '../lib/formatCell'
import { useMotion } from './useMotion'
import { valueScale, frac as scaleFrac, tickDigits } from './scale'
import { buildXAxis } from './xaxis'
import { useChartCursor } from './useChartCursor'
import A2UIPlotFrame from './A2UIPlotFrame.vue'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, data } = useLiveQuery(() => props.component)
const livePoints = computed<Point[]>(() => (data.value ? toPoints(data.value.columns, data.value.rows) : []))

const points = computed<Point[]>(() => (isBound(props.component) ? livePoints.value : (props.component.points ?? [])))

const W = 640
const H = 200
const PAD = { top: 10, right: 12, bottom: 10, left: 12 }
const innerW = W - PAD.left - PAD.right
const innerH = H - PAD.top - PAD.bottom

const values = computed(() => points.value.map((p) => p.value))
const scale = computed(() => valueScale(values.value, { zero: props.component.zero !== false }))
const digits = computed(() => tickDigits(scale.value.step))
const fmtTick = (v: number) => formatNumber(v, digits.value)

// The line rises from the bottom gridline with the component's "motion"
// preset; axis labels and tooltips always show the real values.
const { values: anim } = useMotion(() => values.value.map((v) => v - scale.value.min), () => props.component.motion)
function animY(i: number): number {
  return y((anim.value[i] ?? values.value[i] - scale.value.min) + scale.value.min)
}

const plotWidthPx = ref(0)
const xa = computed(() => buildXAxis(points.value.map((p) => p.label), plotWidthPx.value, W))

function x(i: number): number {
  return PAD.left + xa.value.frac[i] * innerW
}
function y(v: number): number {
  return PAD.top + innerH - scaleFrac(scale.value, v) * innerH
}

const yTicks = computed(() => scale.value.ticks.map((t) => ({ pct: (y(t) / H) * 100, label: fmtTick(t) })))
const xTicks = computed(() => xa.value.ticks.map((t) => ({ frac: (PAD.left + t.frac * innerW) / W, label: t.label })))
const axw = computed(() => Math.max(2, ...scale.value.ticks.map((t) => fmtTick(t).length)) + 1)

// Cursor: positions run left to right (xa.order); `point` maps back to the data.
const orderedFracs = computed(() => xa.value.order.map((i) => xa.value.frac[i]))
const cursor = useChartCursor(() => orderedFracs.value, { W, padL: PAD.left, innerW })
const activeIdx = computed(() => (cursor.active.value === null ? null : (xa.value.order[cursor.active.value] ?? null)))
const activeText = computed(() => (activeIdx.value === null ? '' : `${points.value[activeIdx.value].label}: ${formatNumber(points.value[activeIdx.value].value)}`))
const tipStyle = computed(() => {
  const i = activeIdx.value
  if (i === null) return {}
  const f = (x(i) / W) * 100
  return { left: f + '%', top: (animY(i) / H) * 100 + '%' }
})
const tipSide = computed(() => (activeIdx.value !== null && xa.value.frac[activeIdx.value] > 0.6 ? 'left' : 'right'))

const showDots = computed(() => points.value.length <= 60)
const ariaLabel = computed(() => {
  const v = values.value
  const span = xa.value.time && xa.value.ticks.length ? `, ${points.value[xa.value.order[0]].label} to ${points.value[xa.value.order[xa.value.order.length - 1]].label}` : ''
  return `Line chart of ${v.length} points${span}, ranging ${formatNumber(Math.min(...v))} to ${formatNumber(Math.max(...v))}`
})
const polylinePoints = computed(() => xa.value.order.map((i) => `${x(i)},${animY(i)}`).join(' '))
</script>

<template>
  <div class="a2ui-linechart">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="isBound(component) && loading" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="isBound(component) && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="points.length === 0" class="a2ui-empty">NO ROWS</div>
    <A2UIPlotFrame
      v-else
      data-testid="a2ui-linechart-plot" :y-ticks="yTicks" :x-ticks="xTicks" :axw="axw"
      tabindex="0" role="group" aria-label="Chart values. Use the left and right arrow keys to read each point."
      @measure="plotWidthPx = $event"
      @mousemove="cursor.onMove" @mouseleave="cursor.clear" @keydown="cursor.onKey" @focus="cursor.onFocus" @blur="cursor.clear"
    >
      <svg :viewBox="`0 0 ${W} ${H}`" :class="['a2ui-linechart__svg', colorClass(component.color)]" role="img" :aria-label="ariaLabel">
        <line
          v-for="(t, i) in scale.ticks" :key="'g' + i" :x1="PAD.left" :x2="W - PAD.right" :y1="y(t)" :y2="y(t)"
          class="a2ui-linechart__grid" :class="{ 'a2ui-linechart__grid--zero': t === 0 && scale.min < 0 }"
        />
        <line v-if="activeIdx !== null" :x1="x(activeIdx)" :x2="x(activeIdx)" :y1="PAD.top" :y2="H - PAD.bottom" class="a2ui-linechart__cursor" />
        <polyline class="a2ui-linechart__line" :points="polylinePoints" />
        <template v-if="showDots">
          <circle v-for="(p, i) in points" :key="'dot' + i" :cx="x(i)" :cy="animY(i)" r="3.5" class="a2ui-linechart__dot">
            <title>{{ p.label }}: {{ formatNumber(p.value) }}</title>
          </circle>
        </template>
        <circle v-if="activeIdx !== null" :cx="x(activeIdx)" :cy="animY(activeIdx)" r="5.5" class="a2ui-linechart__focus" />
      </svg>
      <div v-if="activeIdx !== null" class="a2ui-linechart__tip" :class="`a2ui-linechart__tip--${tipSide}`" :style="tipStyle" data-testid="a2ui-linechart-tip">
        <span class="a2ui-linechart__tip-label">{{ points[activeIdx].label }}</span>
        <span class="a2ui-linechart__tip-value">{{ formatNumber(points[activeIdx].value) }}</span>
      </div>
      <span class="a2ui-sr" aria-live="polite">{{ activeText }}</span>
    </A2UIPlotFrame>
  </div>
</template>
