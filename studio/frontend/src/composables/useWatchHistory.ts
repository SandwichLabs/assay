// useWatchHistory — the history panel of one watch (E7 W2): its recorded checks,
// newest first, from the machine-local history (outcome, value or row count and
// reason; never rows). Opening another watch replaces it; refresh() re-reads the
// open one after a check.
import { ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'

const LIMIT = 50

export function useWatchHistory() {
  const name = ref('')
  const entries = ref<WatchEntry[]>([])
  const loading = ref(false)
  const error = ref('')

  async function refresh() {
    const n = name.value
    if (!n) return
    try {
      const rows = await App.WatchHistory(n, LIMIT)
      if (name.value === n) { entries.value = rows; error.value = '' }
    } catch (e) {
      if (name.value === n) { entries.value = []; error.value = errorText(e) }
    } finally {
      if (name.value === n) loading.value = false
    }
  }

  async function open(n: string) {
    name.value = n
    entries.value = []
    loading.value = true
    await refresh()
  }

  function close() {
    name.value = ''
    entries.value = []
    error.value = ''
  }

  return { name, entries, loading, error, open, close, refresh }
}
