<script setup lang="ts">
// A2UISeriesTip — the hover/keyboard readout of a line chart: the label under
// the cursor and one value per series that has a point there (a swatch and the
// series name when there are several).
import { seriesClass } from './series'

defineProps<{
  label: string
  rows: { si: number; name: string; value: string }[]
  multi: boolean
  side: 'left' | 'right'
  pos: { left?: string; top?: string }
}>()
</script>

<template>
  <div class="a2ui-linechart__tip" :class="`a2ui-linechart__tip--${side}`" :style="pos" data-testid="a2ui-linechart-tip">
    <span class="a2ui-linechart__tip-label">{{ label }}</span>
    <span v-if="rows.length === 0" class="a2ui-linechart__tip-name">no values</span>
    <span v-for="r in rows" :key="r.si" class="a2ui-linechart__tip-row" :class="multi ? seriesClass(r.si) : undefined">
      <span v-if="multi" class="a2ui-kchart__swatch"></span>
      <span v-if="multi" class="a2ui-linechart__tip-name">{{ r.name }}</span>
      <span class="a2ui-linechart__tip-value">{{ r.value }}</span>
    </span>
  </div>
</template>
