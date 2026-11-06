// useWatchBadge (E7 W2): the Watches nav badge is the number of watches whose
// newest check fired, and reads 0 when the workspace cannot answer.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { firedWatches, startWatchBadge } from './useWatchBadge'

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useWatchBadge', () => {
  it('shows the count of watches whose newest check fired', async () => {
    install({ ListWatches: vi.fn(async () => ({ watches: [], alerts: 2 })) })
    const stop = startWatchBadge()
    await vi.waitFor(() => expect(firedWatches.value).toBe(2))
    stop()
  })

  it('reads 0 when the workspace cannot answer', async () => {
    install({ ListWatches: vi.fn(async () => { throw new Error('no workspace') }) })
    firedWatches.value = 5
    const stop = startWatchBadge()
    await vi.waitFor(() => expect(firedWatches.value).toBe(0))
    stop()
  })
})
