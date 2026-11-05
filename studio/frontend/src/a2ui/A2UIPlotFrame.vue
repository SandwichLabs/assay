<script setup lang="ts">
// A2UIPlotFrame — the axes around a line or area plot: value labels in a
// column to the left of the plot, tick labels along the bottom. The labels are
// HTML, not SVG <text>, so they stay 11 real CSS px however far the SVG scales
// down (a 640-unit viewBox in a 300px card would shrink SVG text to ~5px).
// y labels sit at a percentage of the plot's height and x labels at a
// percentage of its width; the SVG keeps its viewBox aspect ratio, so those
// percentages land on the same gridlines at any width. Attributes (tabindex,
// key and pointer handlers) go to the plot box, not the wrapper.
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { tickAnchor, type XTick } from './xaxis'

defineOptions({ inheritAttrs: false })
defineProps<{
  yTicks: { pct: number; label: string }[]
  xTicks: XTick[]
  // width of the y label column, in characters of the 11px mono label font
  axw: number
}>()
const emit = defineEmits<{ measure: [widthPx: number] }>()

const xRow = ref<HTMLElement | null>(null)
let ro: ResizeObserver | undefined
function measure() {
  emit('measure', xRow.value?.clientWidth || 0)
}
onMounted(() => {
  measure()
  if (typeof ResizeObserver !== 'undefined' && xRow.value) {
    ro = new ResizeObserver(measure)
    ro.observe(xRow.value)
  }
})
onBeforeUnmount(() => ro?.disconnect())
</script>

<template>
  <div class="a2ui-plot" :style="{ '--a2ui-axw': axw + 'ch' }">
    <div class="a2ui-plot__yaxis" aria-hidden="true">
      <div v-for="(t, i) in yTicks" :key="i" class="a2ui-linechart__axis-y a2ui-plot__axis-y" :style="{ top: t.pct + '%' }">{{ t.label }}</div>
    </div>
    <div class="a2ui-plot__body" v-bind="$attrs">
      <slot />
    </div>
    <div ref="xRow" class="a2ui-linechart__axis-x-row a2ui-plot__xrow" aria-hidden="true">
      <div v-for="(t, i) in xTicks" :key="i" class="a2ui-linechart__axis-x" :style="tickAnchor(t.frac)">{{ t.label }}</div>
    </div>
  </div>
</template>
