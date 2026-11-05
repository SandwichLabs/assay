<script setup lang="ts">
// A2UISeriesLegend — the key of a multi-series line or bar chart. Swatches
// come from the closed palette classes (series.seriesClass), never from the
// page. A folded "Other" says what it holds, so a reader knows it is a sum.
import { seriesClass } from './series'

defineProps<{ names: string[]; folded: string[] }>()
</script>

<template>
  <div v-if="names.length > 1" class="a2ui-kchart__legend" data-testid="a2ui-series-legend">
    <span v-for="(name, i) in names" :key="i" class="a2ui-kchart__key" :class="seriesClass(i)">
      <span class="a2ui-kchart__swatch"></span>{{ name }}
    </span>
    <span v-if="folded.length" class="a2ui-kchart__note" data-testid="a2ui-series-folded" :title="folded.join(', ')">
      Other is the sum of {{ folded.length }} smaller series
    </span>
  </div>
</template>
