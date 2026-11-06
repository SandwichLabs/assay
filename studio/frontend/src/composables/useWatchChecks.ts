// useWatchChecks — the person's checks of their watches (E7 W2). One binding
// runs the named watches (none: all) through the read-only policy, one at a time,
// and records each outcome in the history. Nothing here can change data. While it
// runs, `watches:progress` (a name and a status, never rows) says which watch is
// running and how the finished ones came out, so rows update live and the rest
// read "queued". The results of this session are kept by watch name because they
// carry the sample rows of an alerting "rows" check, which exist only in the
// result and are never stored. Stop ends the run: the watches checked before it
// keep their results and the one it cut short is "stopped", for this window only.
import { computed, onUnmounted, ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { checkSummary } from '../lib/watchText'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

const RUN_KEY = 'watches' // studio/app_watches.go runKeyWatches

export function useWatchChecks(onDone: () => void | Promise<void> = () => {}) {
  const results = ref<Record<string, WatchResult>>({})
  // The names being checked; an empty list is "all" (every row is in the run); null is idle.
  const running = ref<string[] | null>(null)
  // The progress of the run in flight by watch name: "running", or a finished status.
  const progress = ref<Record<string, string>>({})
  const error = ref('')
  const cancelled = ref(false)
  // What the last run found, as one sentence for a live region.
  const summary = ref('')
  const elapsed = ref(0)
  let ticker: ReturnType<typeof setInterval> | undefined
  function stopTicker() { if (ticker) { clearInterval(ticker); ticker = undefined } }

  const busy = computed(() => running.value !== null)
  const checkingAll = computed(() => running.value?.length === 0)
  const isChecking = (name: string) => !!running.value && (running.value.length === 0 || running.value.includes(name))
  /** Where a watch is in the run in flight: "queued", "running", a finished status, or '' when it is not in the run. */
  const phase = (name: string) => (isChecking(name) ? progress.value[name] ?? 'queued' : '')

  // Leaving the view mid-run stops it: the Go run is registered by key and would run on unseen.
  onUnmounted(() => {
    stopTicker()
    if (busy.value) void App.CancelRun(RUN_KEY)
  })

  /** Check the named watches; an empty list checks every watch. One run at a time. */
  async function check(names: string[] = []) {
    if (busy.value) return
    running.value = [...names]
    progress.value = {}
    error.value = ''
    cancelled.value = false
    summary.value = ''
    elapsed.value = 0
    ticker = setInterval(() => { elapsed.value += 1 }, 1000)
    const off = hasWailsRuntime()
      ? EventsOn('watches:progress', (p?: { name?: string; status?: string }) => {
        if (p?.name && p.status) progress.value = { ...progress.value, [p.name]: p.status }
      })
      : () => {}
    try {
      const run = await App.CheckWatches(names)
      const next = names.length ? { ...results.value } : {}
      for (const r of run.results) next[r.watch] = r
      results.value = next
      cancelled.value = !!run.cancelled
      summary.value = checkSummary(run.results, cancelled.value)
    } catch (e) {
      error.value = errorText(e)
    } finally {
      off()
      stopTicker()
      running.value = null
      progress.value = {}
      await onDone()
    }
  }

  /** Stop the check in flight; the results so far are kept. */
  function stop() {
    if (busy.value) void App.CancelRun(RUN_KEY)
  }

  /** Forget the results of this session for a watch (it was saved or deleted: they describe the old file). */
  function forget(name: string) {
    const rest = { ...results.value }
    delete rest[name]
    results.value = rest
  }

  return { results, running, busy, checkingAll, isChecking, phase, progress, error, cancelled, summary, elapsed, check, stop, forget }
}
