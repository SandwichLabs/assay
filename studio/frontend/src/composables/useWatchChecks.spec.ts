// useWatchChecks (E7 W2): a check runs the named watches (none: all), keeps the
// results by name (they carry the sample rows), shows Stop's partial run as
// cancelled rather than failed, and runs one at a time.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, expect, it, vi } from 'vitest'

const handlers = vi.hoisted(() => ({} as Record<string, (p?: unknown) => void>))
const offs = vi.hoisted(() => ({ n: 0 }))
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p?: unknown) => void) => { handlers[name] = cb; return () => { offs.n += 1 } },
}))

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

  it('a failed check reports its error and clears it on the next run', async () => {
    install({ CheckWatches: vi.fn().mockRejectedValueOnce(new Error('watch "nope" not found')).mockResolvedValueOnce({ results: [] }) })
    const c = useWatchChecks()
    await c.check(['nope'])
    expect(c.error.value).toBe('watch "nope" not found')
    expect(c.busy.value).toBe(false)
    await c.check()
    expect(c.error.value).toBe('')
  })

  it('rows read queued until their progress arrives, then running, then the status they finished with', async () => {
    ;(window as any).runtime = {}
    let finish: (v: WatchCheckRun) => void = () => {}
    install({ CheckWatches: () => new Promise<WatchCheckRun>((r) => { finish = r }) })
    offs.n = 0
    const c = useWatchChecks()
    const run = c.check()
    expect(c.phase('a')).toBe('queued')
    handlers['watches:progress']({ name: 'a', status: 'running' })
    expect(c.phase('a')).toBe('running')
    expect(c.phase('b')).toBe('queued')
    handlers['watches:progress']({ name: 'a', status: 'alert' })
    expect(c.phase('a')).toBe('alert')
    finish({ results: [res('a', 'alert'), res('b', 'ok')] })
    await run
    expect(c.phase('a')).toBe('')
    expect(c.summary.value).toBe('Checked 2 watches: 1 fired.')
    expect(offs.n).toBe(1)
    delete (window as any).runtime
  })

  it('a named check puts only that watch in the run', async () => {
    let finish: (v: WatchCheckRun) => void = () => {}
    install({ CheckWatches: () => new Promise<WatchCheckRun>((r) => { finish = r }) })
    const c = useWatchChecks()
    const run = c.check(['a'])
    expect(c.phase('a')).toBe('queued')
    expect(c.phase('b')).toBe('')
    finish({ results: [] })
    await run
  })

  it('a stopped run is summarised as stopped and keeps the interrupted result marked', async () => {
    install({ CheckWatches: vi.fn(async () => ({ results: [res('a', 'ok'), res('b', 'error', { interrupted: true })], cancelled: true })) })
    const c = useWatchChecks()
    await c.check()
    expect(c.results.value.b.interrupted).toBe(true)
    expect(c.summary.value).toBe('Stopped. Checked 1 watch: all ok.')
  })

  it('forgets a deleted watch', async () => {
    install({ CheckWatches: vi.fn(async () => ({ results: [res('a', 'ok'), res('b', 'ok')] })) })
    const c = useWatchChecks()
    await c.check()
    c.forget('a')
    expect(Object.keys(c.results.value)).toEqual(['b'])
  })
})
