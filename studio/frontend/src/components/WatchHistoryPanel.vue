<script setup lang="ts">
// WatchHistoryPanel — one watch's recorded checks (E7 W2), newest first: when,
// outcome, what it read and why. The history is machine-local, keeps the newest
// 200 checks per watch and never holds result rows; an error's reason is its
// class only (a database error can quote a value).
import { durationText, readText, statusBadge, whenText } from '../lib/watchText'

defineProps<{ name: string; entries: WatchEntry[]; loading: boolean; error: string }>()
defineEmits<{ close: [] }>()
</script>

<template>
  <section class="hist" aria-labelledby="wh-h" data-testid="watch-history">
    <div class="hist__head">
      <h3 id="wh-h" class="hist__title">History of <code>{{ name }}</code></h3>
      <button class="btn btn-sm" type="button" data-testid="history-close" @click="$emit('close')">Close</button>
    </div>
    <div v-if="loading" class="help">Reading the history…</div>
    <div v-else-if="error" class="note attn" role="alert"><span>{{ error }}</span></div>
    <p v-else-if="!entries.length" class="help" data-testid="history-empty">This watch has not been checked on this computer yet.</p>
    <div v-else class="table-wrap">
      <table class="table" :aria-label="`Checks of ${name}`" data-testid="history-table">
        <thead><tr><th scope="col">When</th><th scope="col">Outcome</th><th scope="col">Read</th><th scope="col">Reason</th><th scope="col" class="cell-num">Took</th></tr></thead>
        <tbody>
          <tr v-for="(e, i) in entries" :key="e.at + i" :data-testid="`history-row-${i}`">
            <td>{{ whenText(e.at) }}</td>
            <td><span class="badge" :class="statusBadge(e.status)"><span class="dot"></span>{{ e.status }}</span></td>
            <td>{{ readText(e) }}</td>
            <td class="hist__reason">{{ e.reason }}</td>
            <td class="cell-num">{{ durationText(e.duration_ms) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.hist { display: flex; flex-direction: column; gap: var(--sh-space-3); border: var(--sh-border-thin); border-radius: var(--sh-radius-sm); padding: var(--sh-space-4); min-width: 0; }
.hist__head { display: flex; justify-content: space-between; align-items: center; gap: var(--sh-space-3); }
.hist__title { margin: 0; font-size: var(--sh-fs-base); overflow-wrap: anywhere; }
.hist__reason { overflow-wrap: anywhere; min-width: 160px; }
.table-wrap { overflow-x: auto; max-width: 100%; }
.help { margin: 0; }
</style>
