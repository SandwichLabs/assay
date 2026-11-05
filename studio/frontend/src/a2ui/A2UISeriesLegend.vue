<script setup lang="ts">
// A2UISeriesLegend — the key of a multi-series line or bar chart. Swatches
// come from the closed palette classes (series.seriesClass), never from the
// page. Bars sum the series past six into "Other" and say so; lines draw the
// six largest and say how many are not shown, naming them in the title.
import { seriesClass } from './series'

defineProps<{ names: string[]; folded: string[]; mode: 'sum' | 'drop' }>()
</script>

<template>
  <div v-if="names.length > 1" class="a2ui-kchart__legend" data-testid="a2ui-series-legend">
    <span v-for="(name, i) in names" :key="i" class="a2ui-kchart__key" :class="seriesClass(i)">
      <span class="a2ui-kchart__swatch"></span>{{ name }}
    </span>
    <span v-if="folded.length" class="a2ui-kchart__note" data-testid="a2ui-series-folded" :title="folded.join(', ')">
      <template v-if="mode === 'drop'">+{{ folded.length }} more not shown; filter in SQL</template>
      <template v-else>Other is the sum of {{ folded.length }} series</template>
    </span>
  </div>
</template>
