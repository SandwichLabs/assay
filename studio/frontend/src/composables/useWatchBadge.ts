// useWatchBadge — the number of watches whose newest check fired, for the
// Watches nav item's badge. One shared count, read when the shell starts and
// kept live: a check finishing (`watches:checked`), a watch saved or deleted
// (`data:changed`) and a watch file changed by another process (a watcher
// event) all re-read it. The shell reads it again when another workspace opens.
// It comes from the machine-local history, so it is the same count on every
// start of the app and says nothing until a check has run.
import { ref } from 'vue'
import { App } from '../lib/bindings'
import { hasWailsRuntime } from '../lib/wails'
import { EventsOn } from '../wailsjs/runtime/runtime'

export const firedWatches = ref(0)

let timer: ReturnType<typeof setTimeout> | undefined

/** Re-read soon: a burst of events makes one read. */
function refreshSoon() {
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => { timer = undefined; void refreshWatchBadge() }, 250)
}

export async function refreshWatchBadge() {
  try {
    firedWatches.value = (await App.ListWatches()).alerts
  } catch {
    firedWatches.value = 0
  }
}

/** Start keeping the count live; returns the function that stops it. */
export function startWatchBadge(): () => void {
  void refreshWatchBadge()
  if (!hasWailsRuntime()) return () => {}
  const offs = [
    EventsOn('watches:checked', refreshSoon),
    EventsOn('data:changed', refreshSoon),
    EventsOn('workspace:changed-on-disk', (p?: { kind?: string }) => { if (p?.kind === 'watch' || p?.kind === 'watch_history') refreshSoon() }),
  ]
  return () => { if (timer) clearTimeout(timer); for (const off of offs) off() }
}
