<script setup lang="ts">
// A2UIAnnotations — the event markers of a line, area or column chart: a
// vertical rule per marker across the plot, its label in a row above the plot
// (staggered by annotations.ts so close dates do not collide), and a tooltip with
// the full label and the exact `at` when the pointer is on a rule or label.
// The markers are placed and laid out by the chart (useAnnotations); this only
// draws them. The component is decorative to assistive tech (aria-hidden): the
// chart's own text alternative lists the markers. Colour comes from the closed
// a2ui--<token> classes, never from a style attribute.
import { ref } from 'vue'
import { colorClass } from './query'
import { LANE_PX, describe, type LaidMarker } from './annotations'

defineProps<{
  markers: LaidMarker[]
  // where the plot sits inside the box this is placed in, as CSS lengths
  inset: { top: string; right: string; bottom: string; left: string }
}>()
const hover = ref<number | null>(null)
</script>

<template>
  <div
    class="a2ui-annots" aria-hidden="true" data-testid="a2ui-annotations"
    :style="{ top: inset.top, right: inset.right, bottom: inset.bottom, left: inset.left }"
  >
    <div
      v-for="m in markers" :key="m.key" class="a2ui-annot"
      :class="[colorClass(m.ann.color), `a2ui-annot--${m.ann.kind}`, { 'a2ui-annot--active': hover === m.key }]"
      :style="{ left: m.frac * 100 + '%', top: (m.lane === null ? 0 : -(m.lane + 1) * LANE_PX) + 'px' }"
      data-testid="a2ui-annotation" :data-at="m.ann.at"
      @mouseenter="hover = m.key" @mouseleave="hover = null"
    >
      <span v-if="m.lane !== null" class="a2ui-annot__label" :style="{ left: m.shift + 'px', maxWidth: m.width + 'px' }">{{ m.text }}</span>
    </div>
    <template v-for="m in markers" :key="'tip' + m.key">
      <div
        v-if="hover === m.key" class="a2ui-annot__tip" :class="m.frac > 0.6 ? 'a2ui-annot__tip--left' : 'a2ui-annot__tip--right'"
        :style="{ left: m.frac * 100 + '%' }" data-testid="a2ui-annotation-tip"
      >
        <span class="a2ui-annot__tip-label">{{ m.ann.label }}</span>
        <span v-if="m.ann.label.trim() !== m.ann.at" class="a2ui-annot__tip-at">{{ m.ann.at }}</span>
        <span class="a2ui-sr">{{ describe(m) }}</span>
      </div>
    </template>
  </div>
</template>
