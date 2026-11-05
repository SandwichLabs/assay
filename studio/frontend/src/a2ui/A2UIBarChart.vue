<script setup lang="ts">
// A2UIBarChart — zero-baselined horizontal bars, inline "bars" or a live
// "sql" binding. Ported from renderer.js's renderBarChart/appendBars.
//
// Zero-baselined on purpose: bar length encodes magnitude, and starting the
// axis at the smallest value would make a 233-vs-208 gap look like 10x. A
// negative value draws leftward from the zero line. The value axis along the
// bottom and the faint gridlines in each track use the shared round ticks
// (a2ui/scale.ts). Bar offsets and widths are plain numbers (clamped
// percentages) bound only through numeric :style — never a raw model string.
// Bars grow out of the zero line with the component's "motion" preset.
import { computed } from 'vue'
import type { A2UIComponentT } from './types'
import { toPoints, colorClass, type Point, isBound } from './query'
import { useLiveQuery } from './useLiveQuery'
import { formatNumber } from '../lib/formatCell'
import { useMotion } from './useMotion'
import { useDrill } from './useDrill'
import { useRoving } from './useRoving'
import { valueScale, frac as scaleFrac, tickDigits } from './scale'
import { tickAnchor } from './xaxis'

const props = defineProps<{ component: A2UIComponentT }>()
const { loading, error, data } = useLiveQuery(() => props.component)
const livePoints = computed<Point[]>(() => (data.value ? toPoints(data.value.columns, data.value.rows) : []))

const points = computed<Point[]>(() => (isBound(props.component) ? livePoints.value : (props.component.bars ?? [])))
const values = computed(() => points.value.map((p) => p.value))
const scale = computed(() => valueScale(values.value))
const zeroPct = computed(() => scaleFrac(scale.value, 0) * 100)
// Animate from zero, so a bar grows out of the zero line.
const { values: anim } = useMotion(() => values.value, () => props.component.motion)

const digits = computed(() => tickDigits(scale.value.step))
const fmtTick = (v: number) => formatNumber(v, digits.value)
const tickPcts = computed(() => scale.value.ticks.map((t) => ({ pct: scaleFrac(scale.value, t) * 100, label: fmtTick(t), zero: t === 0 })))
// One shared value-column width, so every track is the same length.
const valueCh = computed(() => Math.max(4, ...values.value.map((v) => formatNumber(v).length)) + 1)

// Drill (data artifact view only): a bar is one result row; click, or Enter
// or Space on a focused bar, sets the drill parameter from that row's column.
const drill = useDrill(() => props.component, () => (isBound(props.component) && data.value ? data.value : null))
const roving = useRoving(() => points.value.length)

// The bar spans zero to the value; at least 1% wide so a tiny value is visible.
function barStyle(i: number): { left: string; width: string } {
  const v = anim.value[i] ?? values.value[i]
  const end = Math.min(Math.max(scaleFrac(scale.value, v) * 100, 0), 100)
  const lo = Math.min(end, zeroPct.value)
  const w = Math.min(Math.max(Math.abs(end - zeroPct.value), 1), 100)
  return { left: Math.min(lo, 100 - w) + '%', width: w + '%' }
}
</script>

<template>
  <div class="a2ui-barchart" @keydown="drill.enabled.value && roving.onKeydown($event)" @focusin="roving.onFocusin">
    <div v-if="component.label" class="a2ui-component__title">{{ component.label }}</div>
    <div v-if="isBound(component) && loading && !data" class="a2ui-loading">RUNNING QUERY…</div>
    <div v-else-if="isBound(component) && error" class="a2ui-error">QUERY ERROR: {{ error }}</div>
    <div v-else-if="points.length === 0" class="a2ui-empty">NO ROWS</div>
    <template v-else>
      <div v-if="drill.problem.value" class="a2ui-error" role="alert">{{ drill.problem.value }}</div>
      <div
        v-for="(bar, i) in points" :key="i" class="a2ui-bar" :title="`${bar.label}: ${formatNumber(bar.value)}`"
        :class="{ 'a2ui-drill-row': drill.enabled.value, 'a2ui-drill-row--current': drill.isCurrent(i) }"
        v-bind="drill.enabled.value ? { role: 'button', 'data-roving': '', 'data-roving-i': i, tabindex: roving.tab(i), 'aria-current': drill.isCurrent(i) ? 'true' : undefined, 'aria-label': `${bar.label}: ${formatNumber(bar.value)}. ${drill.action(i)}` } : {}"
        @click="drill.drill(i)"
        @keydown="drill.enabled.value && drill.onKey($event, i)"
      >
        <span class="a2ui-bar__label">{{ bar.label }}</span>
        <div class="a2ui-bar__track">
          <span v-for="(t, ti) in tickPcts" :key="ti" class="a2ui-bar__grid" :class="{ 'a2ui-bar__grid--zero': t.zero && scale.min < 0 }" :style="{ left: t.pct + '%' }" aria-hidden="true"></span>
          <div class="a2ui-bar__fill" :class="colorClass(component.color)" :style="barStyle(i)"></div>
        </div>
        <span class="a2ui-bar__value" :style="{ minWidth: valueCh + 'ch' }">{{ formatNumber(bar.value) }}</span>
      </div>
      <div class="a2ui-baraxis" aria-hidden="true">
        <span class="a2ui-baraxis__label"></span>
        <div class="a2ui-bar__axis">
          <span v-for="(t, ti) in tickPcts" :key="ti" class="a2ui-linechart__axis-x" :style="tickAnchor(t.pct / 100)">{{ t.label }}</span>
        </div>
        <span class="a2ui-baraxis__value" :style="{ minWidth: valueCh + 'ch' }"></span>
      </div>
      <div v-if="isBound(component) && loading" class="a2ui-loading" role="status">RUNNING QUERY…</div>
      <div v-if="drill.error.value" class="a2ui-error" role="alert" data-testid="a2ui-drill-error">{{ drill.error.value }}</div>
    </template>
  </div>
</template>
