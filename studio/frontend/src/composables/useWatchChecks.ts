// useWatchChecks — the person's checks of their watches (E7 W2). One binding
// runs the named watches (none: all) through the read-only policy, one at a time,
// and records each outcome in the history. Nothing here can change data. The
// results of this session are kept by watch name because they carry the sample
// rows of an alerting "rows" check, which exist only in the result and are never
// stored. Stop ends the run; the watches checked before it keep their results.
import { computed, onUnmounted, ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText, isCancelled } from '../lib/errors'

const RUN_KEY = 'watches' // studio/app_watches.go runKeyWatches

export function useWatchChecks(onDone: () => void | Promise<void> = () => {}) {
  const results = ref<Record<string, WatchResult>>({})
  // The names being checked; an empty list is "all" (every row shows as checking); null is idle.
  const running = ref<string[] | null>(null)
  const error = ref('')
  const cancelled = ref(false)
  const elapsed = ref(0)
  let ticker: ReturnType<typeof setInterval> | undefined
  function stopTicker() { if (ticker) { clearInterval(ticker); ticker = undefined } }

  const busy = computed(() => running.value !== null)
  const checkingAll = computed(() => running.value?.length === 0)
  const isChecking = (name: string) => !!running.value && (running.value.length === 0 || running.value.includes(name))

  // Leaving the view mid-run stops it: the Go run is registered by key and would run on unseen.
  onUnmounted(() => {
    stopTicker()
    if (busy.value) void App.CancelRun(RUN_KEY)
  })

  /** Check the named watches; an empty list checks every watch. One run at a time. */
  async function check(names: string[] = []) {
    if (busy.value) return
    running.value = [...names]
    error.value = ''
    cancelled.value = false
    elapsed.value = 0
    ticker = setInterval(() => { elapsed.value += 1 }, 1000)
    try {
      const run = await App.CheckWatches(names)
      const next = names.length ? { ...results.value } : {}
      for (const r of run.results) next[r.watch] = r
      results.value = next
      cancelled.value = !!run.cancelled
    } catch (e) {
      if (isCancelled(e)) cancelled.value = true
      else error.value = errorText(e)
    } finally {
      stopTicker()
      running.value = null
      await onDone()
    }
  }

  /** Stop the check in flight; the results so far are kept. */
  function stop() {
    if (busy.value) void App.CancelRun(RUN_KEY)
  }

  /** Forget the results of this session for a watch (it was deleted). */
  function forget(name: string) {
    const rest = { ...results.value }
    delete rest[name]
    results.value = rest
  }

  return { results, running, busy, checkingAll, isChecking, error, cancelled, elapsed, check, stop, forget }
}
