// useWatchHistory (E7 W2): the history panel of one watch. Opening another watch
// while the first is still loading must not let the slow first answer land in the
// second's panel, and a failure of an old read must not show on the new one.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, expect, it, vi } from 'vitest'
import { useWatchHistory } from './useWatchHistory'

const entry = (watch: string, reason: string): WatchEntry => ({ watch, at: '2026-11-06T20:00:00Z', status: 'ok', reason, duration_ms: 1 })

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useWatchHistory', () => {
  it('opens a watch, asks for its newest 50 and closes', async () => {
    const WatchHistory = vi.fn(async (n: string) => [entry(n, 'fine')])
    install({ WatchHistory })
    const h = useWatchHistory()
    await h.open('a')
    expect(WatchHistory).toHaveBeenCalledWith('a', 50)
    expect(h.entries.value.map((e) => e.reason)).toEqual(['fine'])
    expect(h.loading.value).toBe(false)
    h.close()
    expect(h.name.value).toBe('')
    expect(h.entries.value).toEqual([])
  })

  it('switching rows mid-load keeps the second watch, whichever read ends last', async () => {
    const pending: Record<string, (v: WatchEntry[]) => void> = {}
    install({ WatchHistory: vi.fn((n: string) => new Promise<WatchEntry[]>((r) => { pending[n] = r })) })
    const h = useWatchHistory()
    const first = h.open('a')
    const second = h.open('b')
    expect(h.loading.value).toBe(true)
    pending.b([entry('b', 'from b')])
    await second
    pending.a([entry('a', 'from a')])
    await first
    expect(h.name.value).toBe('b')
    expect(h.entries.value.map((e) => e.reason)).toEqual(['from b'])
    expect(h.loading.value).toBe(false)
  })

  it('a failure of an older read does not show on the newer one', async () => {
    const fail: Record<string, (e: Error) => void> = {}
    const ok: Record<string, (v: WatchEntry[]) => void> = {}
    install({ WatchHistory: vi.fn((n: string) => new Promise<WatchEntry[]>((res, rej) => { ok[n] = res; fail[n] = rej })) })
    const h = useWatchHistory()
    const first = h.open('a')
    const second = h.open('b')
    fail.a(new Error('history is unreadable'))
    await first
    expect(h.error.value).toBe('')
    ok.b([entry('b', 'fine')])
    await second
    expect(h.entries.value).toHaveLength(1)
  })

  it('shows an error and no rows when the read fails, and refresh re-reads the open watch', async () => {
    const WatchHistory = vi.fn().mockRejectedValueOnce(new Error('failed to read the history: bad')).mockResolvedValueOnce([entry('a', 'now fine')])
    install({ WatchHistory })
    const h = useWatchHistory()
    await h.open('a')
    expect(h.error.value).toBe('bad')
    expect(h.entries.value).toEqual([])
    await h.refresh()
    expect(h.error.value).toBe('')
    expect(h.entries.value.map((e) => e.reason)).toEqual(['now fine'])
  })
})
