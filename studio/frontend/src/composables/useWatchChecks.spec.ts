// useWatchChecks (E7 W2): a check runs the named watches (none: all), keeps the
// results by name (they carry the sample rows), shows Stop's partial run as
// cancelled rather than failed, and runs one at a time.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, expect, it, vi } from 'vitest'
import { useWatchChecks } from './useWatchChecks'

const res = (watch: string, status: WatchResult['status'], over: Partial<WatchResult> = {}): WatchResult => ({
  watch, at: '2026-11-06T20:00:00Z', status, reason: `${watch} ${status}`, duration_ms: 3, question: 'query q', condition: 'any rows', ...over,
})

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

describe('useWatchChecks', () => {
  it('checks all, keeps the results by name and then reloads', async () => {
    const check = vi.fn(async () => ({ results: [res('a', 'ok'), res('b', 'alert', { sample: { columns: ['id'], rows: [['1']] } })] }))
    install({ CheckWatches: check })
    const done = vi.fn()
    const c = useWatchChecks(done)
    await c.check()
    expect(check).toHaveBeenCalledWith([])
    expect(Object.keys(c.results.value)).toEqual(['a', 'b'])
    expect(c.results.value.b.sample?.rows).toEqual([['1']])
    expect(c.busy.value).toBe(false)
    expect(c.cancelled.value).toBe(false)
    expect(done).toHaveBeenCalledTimes(1)
  })

  it('a named check replaces only that watch; checking all starts afresh', async () => {
    const check = vi.fn()
      .mockResolvedValueOnce({ results: [res('a', 'ok'), res('b', 'ok')] })
      .mockResolvedValueOnce({ results: [res('a', 'alert')] })
      .mockResolvedValueOnce({ results: [res('c', 'ok')] })
    install({ CheckWatches: check })
    const c = useWatchChecks()
    await c.check()
    await c.check(['a'])
    expect(c.results.value.a.status).toBe('alert')
    expect(c.results.value.b.status).toBe('ok')
    await c.check()
    expect(Object.keys(c.results.value)).toEqual(['c'])
  })

  it('shows which rows are checking, and does not start a second run', async () => {
    let finish: (v: WatchCheckRun) => void = () => {}
    const check = vi.fn(() => new Promise<WatchCheckRun>((r) => { finish = r }))
    install({ CheckWatches: check })
    const c = useWatchChecks()
    const first = c.check(['a'])
    expect(c.busy.value).toBe(true)
    expect(c.isChecking('a')).toBe(true)
    expect(c.isChecking('b')).toBe(false)
    expect(c.checkingAll.value).toBe(false)
    await c.check(['b'])
    expect(check).toHaveBeenCalledTimes(1)
    finish({ results: [res('a', 'ok')] })
    await first
    expect(c.busy.value).toBe(false)
  })

  it('Stop cancels the run by key and a partial run reads as cancelled', async () => {
    let finish: (v: WatchCheckRun) => void = () => {}
    const cancel = vi.fn()
    install({ CheckWatches: () => new Promise<WatchCheckRun>((r) => { finish = r }), CancelRun: cancel })
    const c = useWatchChecks()
    const run = c.check()
    expect(c.checkingAll.value).toBe(true)
    c.stop()
    expect(cancel).toHaveBeenCalledWith('watches')
    finish({ results: [res('a', 'ok')], cancelled: true })
    await run
    expect(c.cancelled.value).toBe(true)
    expect(c.error.value).toBe('')
    expect(c.results.value.a.status).toBe('ok')
  })

  it('a failed check reports its error; a cancelled error is neutral', async () => {
    install({ CheckWatches: vi.fn().mockRejectedValueOnce(new Error('watch "nope" not found')).mockRejectedValueOnce(new Error('cancelled')) })
    const c = useWatchChecks()
    await c.check(['nope'])
    expect(c.error.value).toBe('watch "nope" not found')
    await c.check()
    expect(c.error.value).toBe('')
    expect(c.cancelled.value).toBe(true)
  })

  it('forgets a deleted watch', async () => {
    install({ CheckWatches: vi.fn(async () => ({ results: [res('a', 'ok'), res('b', 'ok')] })) })
    const c = useWatchChecks()
    await c.check()
    c.forget('a')
    expect(Object.keys(c.results.value)).toEqual(['b'])
  })
})
