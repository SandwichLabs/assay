// useWatches — the Watches view's list (E7 W2). One read lists every file under
// watches/ with its last check from the machine-local history. The list reloads,
// coalesced, when a watch file changes on disk (an editor, git, another
// process), when a check finishes (`watches:checked`) and when Studio saves or
// deletes one (`data:changed`), so the view is never older than its files.
import { onMounted, onUnmounted, ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

const RELOAD_COALESCE_MS = 150

export function useWatches() {
  const list = ref<WatchList | null>(null)
  const loading = ref(true)
  const error = ref('')

  async function reload() {
    try {
      list.value = await App.ListWatches()
      error.value = ''
    } catch (e) {
      error.value = errorText(e)
    } finally {
      loading.value = false
    }
  }

  let timer: ReturnType<typeof setTimeout> | undefined
  let gone = false
  const offs: Array<() => void> = []
  onMounted(() => {
    void reload()
    if (!hasWailsRuntime()) return
    const schedule = () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => { timer = undefined; if (!gone) void reload() }, RELOAD_COALESCE_MS)
    }
    offs.push(EventsOn('workspace:changed-on-disk', (p?: { kind?: string }) => { if (p?.kind === 'watch') schedule() }))
    offs.push(EventsOn('watches:checked', schedule))
    offs.push(EventsOn('data:changed', schedule))
  })
  onUnmounted(() => {
    gone = true
    if (timer) clearTimeout(timer)
    for (const off of offs) off()
  })

  return { list, loading, error, reload }
}
