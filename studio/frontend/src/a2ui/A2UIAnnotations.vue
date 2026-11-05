<script setup lang="ts">
// A2UIAnnotations — the event markers of a line, area or column chart: a
// vertical rule per marker across the plot, its label in a row above the plot
// (staggered by annotations.ts so close dates do not collide), and a tooltip with
// the full label and the exact `at` when the pointer is on a rule or label.
// The markers are placed and laid out by the chart (useAnnotations); this only
// draws them. The component is decorative to assistive tech (aria-hidden): the
// chart's own text alternative lists the markers. Colour comes from the closed
// a2ui--<token> classes, never from a style attribute.
//
// `scroll` is for a chart whose plot scrolls sideways (a columnChart with many
// columns): the markers' fractions are of the scrolling content, and the layer is
// moved by the scroll offset and clipped to the plot's width, so a marker stays on
// its column.
import { computed, ref } from 'vue'
import { colorClass } from './query'
import { LANE_PX, describe, type LaidMarker } from './annotations'

const props = defineProps<{
  markers: LaidMarker[]
  // where the plot sits inside the box this is placed in, as CSS lengths
  inset: { top: string; right: string; bottom: string; left: string }
  scroll?: { width: number; left: number; view: number }
}>()
const hover = ref<number | null>(null)
// room above the plot kept inside the clip box, for the label rows
const HEAD = 100
const rootStyle = computed(() => ({ top: props.inset.top, right: props.inset.right, bottom: props.inset.bottom, left: props.inset.left }))
const innerStyle = computed(() => (props.scroll ? { top: HEAD + 'px', width: props.scroll.width + 'px', left: -props.scroll.left + 'px', right: 'auto' } : {}))
// the tooltip opens to the left of a marker in the right part of what shows
const onRight = (m: LaidMarker) => (props.scroll ? (m.frac * props.scroll.width - props.scroll.left) / Math.max(props.scroll.view, 1) : m.frac) > 0.6
// the tooltip is outside the clip box (a long one is not cut at the panel edge): at the
// marker's visible x, in px when the plot scrolls
const tipLeft = (m: LaidMarker) => (props.scroll ? `${m.frac * props.scroll.width - props.scroll.left}px` : m.frac * 100 + '%')
</script>

<template>
  <div class="a2ui-annots" aria-hidden="true" data-testid="a2ui-annotations" :style="rootStyle">
    <div class="a2ui-annots__clip" :class="{ 'a2ui-annots__clip--scroll': scroll }">
    <div class="a2ui-annots__inner" :style="innerStyle">
      <div
        v-for="m in markers" :key="m.key" class="a2ui-annot"
        :class="[colorClass(m.ann.color), `a2ui-annot--${m.ann.kind}`, { 'a2ui-annot--active': hover === m.key }]"
        :style="{ left: m.frac * 100 + '%', top: (m.lane === null ? 0 : -(m.lane + 1) * LANE_PX) + 'px' }"
        data-testid="a2ui-annotation" :data-at="m.ann.at"
        @mouseenter="hover = m.key" @mouseleave="hover = null"
      >
        <span v-if="m.lane !== null" class="a2ui-annot__label" :style="{ left: m.shift + 'px', maxWidth: m.width + 'px' }">{{ m.text }}</span>
      </div>
    </div>
    </div>
    <template v-for="m in markers" :key="'tip' + m.key">
      <div
        v-if="hover === m.key" class="a2ui-annot__tip" :class="onRight(m) ? 'a2ui-annot__tip--left' : 'a2ui-annot__tip--right'"
        :style="{ left: tipLeft(m) }" data-testid="a2ui-annotation-tip"
      >
        <span class="a2ui-annot__tip-label">{{ m.ann.label }}</span>
        <span v-if="m.ann.label.trim() !== m.ann.at" class="a2ui-annot__tip-at">{{ m.ann.at }}</span>
        <span class="a2ui-sr">{{ describe(m) }}</span>
      </div>
    </template>
  </div>
</template>
