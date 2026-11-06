<script setup lang="ts">
// WatchAlerts — the alert panel (E7 W2): every watch whose newest check fired,
// with the reason and, for a "rows" check run in this window, the first rows it
// read as a small table. The rows exist only in the result of the check in this
// session; the history never keeps them, so after a restart the panel says to
// check again to see them. Cells are shown as text.
import { whenText } from '../lib/watchText'

defineProps<{
  alerts: WatchSummary[]
  // This session's check results by watch name (they carry the sample rows).
  results: Record<string, WatchResult>
}>()
const reasonOf = (w: WatchSummary, r?: WatchResult) => r?.reason ?? w.last?.reason ?? 'The condition holds.'
const atOf = (w: WatchSummary, r?: WatchResult) => whenText(r?.at ?? w.last?.at)
</script>

<template>
  <section class="alerts" aria-labelledby="wa-h" data-testid="watch-alerts">
    <h3 id="wa-h" class="alerts__head">
      <span class="badge badge--warn"><span class="dot"></span>{{ alerts.length }} fired</span>
      Alerts
    </h3>
    <ul class="alerts__list">
      <li v-for="w in alerts" :key="w.name" class="alert-card" :data-testid="`alert-${w.name}`">
        <div class="alert-card__top">
          <b class="alert-card__name">{{ w.name }}</b>
          <span class="alert-card__when">{{ atOf(w, results[w.name]) }}</span>
        </div>
        <p class="alert-card__reason" :data-testid="`alert-reason-${w.name}`">{{ reasonOf(w, results[w.name]) }}</p>
        <div v-if="results[w.name]?.sample" class="alert-card__sample">
          <div class="table-wrap">
            <table class="table" :aria-label="`First rows read by ${w.name}`" :data-testid="`alert-sample-${w.name}`">
              <thead><tr><th v-for="c in results[w.name].sample!.columns" :key="c" scope="col">{{ c }}</th></tr></thead>
              <tbody>
                <tr v-for="(row, i) in results[w.name].sample!.rows" :key="i">
                  <td v-for="(cell, j) in row" :key="j">{{ cell }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <p class="help">
            The first {{ results[w.name].sample!.rows.length }} {{ results[w.name].sample!.rows.length === 1 ? 'row' : 'rows' }}<template v-if="results[w.name].sample!.capped">, trimmed</template>.
            They are shown here only and are not kept in the history.
          </p>
        </div>
        <p v-else-if="w.when.rows" class="help" :data-testid="`alert-nosample-${w.name}`">Check again to see the rows. They are not kept in the history.</p>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.alerts { display: flex; flex-direction: column; gap: var(--sh-space-3); }
.alerts__head { display: flex; align-items: center; gap: var(--sh-space-3); margin: 0; font-size: var(--sh-fs-sm); text-transform: uppercase; letter-spacing: 0.06em; color: var(--sh-graphite); }
.alerts__list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: var(--sh-space-3); }
.alert-card { border: var(--sh-border-thin); border-left: 4px solid var(--sh-marigold); background: var(--sh-marigold-8); border-radius: var(--sh-radius-sm); padding: var(--sh-space-3) var(--sh-space-4); display: flex; flex-direction: column; gap: var(--sh-space-2); min-width: 0; }
.alert-card__top { display: flex; justify-content: space-between; gap: var(--sh-space-3); flex-wrap: wrap; }
.alert-card__name { overflow-wrap: anywhere; }
.alert-card__when { font-size: var(--sh-fs-xs); color: var(--sh-graphite); }
.alert-card__reason { margin: 0; overflow-wrap: anywhere; }
.alert-card__sample { display: flex; flex-direction: column; gap: var(--sh-space-1); min-width: 0; }
.table-wrap { overflow-x: auto; max-width: 100%; }
.help { margin: 0; }
</style>
