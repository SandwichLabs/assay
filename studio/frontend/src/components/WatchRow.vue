<script setup lang="ts">
// WatchRow — one watch in the Watches list (E7 W2): its question and condition,
// the outcome of its last check (this window's, else the history's) with the
// reason, when it last fired, and its actions. A file that cannot be read shows
// its error in place of a question and is not checkable; Edit replaces it.
import { computed } from 'vue'
import { readText, statusBadge, statusLabel, whenText } from '../lib/watchText'

const props = defineProps<{
  watch: WatchSummary
  result?: WatchResult
  checking: boolean
  // A check is running (the others' Check buttons wait for it).
  busy: boolean
  readOnly: boolean
  historyOpen: boolean
}>()
defineEmits<{ check: []; edit: []; history: []; remove: [] }>()

const status = computed(() => (props.watch.error ? 'error' : props.result?.status ?? props.watch.last_status))
const reason = computed(() => props.watch.error || props.result?.reason || props.watch.last?.reason || '')
const read = computed(() => (props.result ? readText(props.result) : props.watch.last ? readText(props.watch.last) : ''))
const checkedAt = computed(() => whenText(props.result?.at ?? props.watch.last_checked))
</script>

<template>
  <li class="wrow" :class="{ 'wrow--alert': status === 'alert' && !watch.error, 'wrow--error': status === 'error' }" :data-testid="`watch-${watch.name}`">
    <div class="wrow__main">
      <b class="wrow__name">{{ watch.name }}</b>
      <span v-if="watch.error" class="wrow__q">This file cannot be read</span>
      <span v-else class="wrow__q"><span class="wrow__ask">{{ watch.question_text }}</span> <span class="wrow__sep" aria-hidden="true">·</span> fires when {{ watch.when_text }}</span>
      <span v-if="watch.description" class="wrow__desc">{{ watch.description }}</span>
    </div>
    <div class="wrow__status" :data-testid="`watch-status-${watch.name}`">
      <span v-if="checking" class="badge badge--info" role="status"><span class="dot"></span>checking…</span>
      <span v-else class="badge" :class="statusBadge(status)"><span class="dot"></span>{{ statusLabel(status) }}</span>
      <span v-if="reason && !checking" class="wrow__reason" :data-testid="`watch-reason-${watch.name}`">{{ reason }}</span>
      <span class="wrow__meta">
        <template v-if="!watch.error">Checked {{ checkedAt }}<template v-if="read"> · read {{ read }}</template> · </template>last fired {{ whenText(watch.last_fired) }}
      </span>
    </div>
    <div class="wrow__actions">
      <button class="btn btn-sm" type="button" :disabled="busy || !!watch.error" :aria-label="`Check ${watch.name}`" :data-testid="`check-${watch.name}`" @click="$emit('check')">Check</button>
      <button class="btn btn-sm" type="button" :aria-pressed="historyOpen" :aria-label="`History of ${watch.name}`" :data-testid="`history-${watch.name}`" @click="$emit('history')">History</button>
      <button class="btn btn-sm" type="button" :disabled="readOnly" :aria-label="`Edit ${watch.name}`" :data-testid="`edit-${watch.name}`" @click="$emit('edit')">Edit</button>
      <button class="btn btn-sm" type="button" :disabled="readOnly" :aria-label="`Delete ${watch.name}`" :data-testid="`delete-${watch.name}`" @click="$emit('remove')">Delete</button>
    </div>
  </li>
</template>

<style scoped>
.wrow { display: grid; grid-template-columns: minmax(240px, 2fr) minmax(240px, 2fr) auto; gap: var(--sh-space-3) var(--sh-space-5); align-items: start; padding: var(--sh-space-3) var(--sh-space-4); border-bottom: var(--sh-border-thin); min-width: 0; }
.wrow--alert { box-shadow: inset 3px 0 0 var(--sh-marigold); }
.wrow--error { box-shadow: inset 3px 0 0 var(--sh-coral); }
.wrow__main, .wrow__status { display: flex; flex-direction: column; gap: var(--sh-space-1); min-width: 0; align-items: flex-start; }
.wrow__name, .wrow__q, .wrow__desc, .wrow__reason { overflow-wrap: anywhere; }
.wrow__q, .wrow__meta { font-size: var(--sh-fs-sm); color: var(--sh-graphite); }
.wrow__ask { font-family: var(--sh-font-mono); }
.wrow__desc { font-size: var(--sh-fs-sm); }
.wrow__reason { font-size: var(--sh-fs-sm); }
.wrow__meta { font-size: var(--sh-fs-xs); }
.wrow__actions { display: flex; gap: var(--sh-space-2); flex-wrap: wrap; justify-content: flex-end; }
@media (max-width: 900px) {
  .wrow { grid-template-columns: 1fr; }
  .wrow__actions { justify-content: flex-start; }
}
</style>
