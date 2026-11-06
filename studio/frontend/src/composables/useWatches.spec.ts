// useWatches (E7 W2): the list reads once on mount, re-reads when a watch file
// changes, a check finishes or Studio saves one, coalesces a burst of events
// into one read, ignores other kinds of files, and stops listening and reading
// once the view is gone.
type MockApp = Partial<Record<keyof AppApi, any>>
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

const handlers = vi.hoisted(() => ({} as Record<string, (p?: unknown) => void>))
const offs = vi.hoisted(() => ({ n: 0 }))
vi.mock('../wailsjs/runtime/runtime', () => ({
  EventsOn: (name: string, cb: (p?: unknown) => void) => { handlers[name] = cb; return () => { offs.n += 1 } },
}))

import { useWatches } from './useWatches'

const list = (n: number): WatchList => ({ watches: [{ name: `w${n}`, question: {}, when: {} }], alerts: 0 })

function host(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  ;(window as any).runtime = {}
  let api!: ReturnType<typeof useWatches>
  const w = mount(defineComponent({ setup() { api = useWatches(); return () => h('div') } }))
  return { w, api: () => api }
}

describe('useWatches', () => {
  beforeEach(() => { vi.useFakeTimers(); offs.n = 0 })
  afterEach(() => { vi.useRealTimers(); delete (window as any).runtime })

  it('reads on mount and shows the list or the error', async () => {
    const ok = host({ ListWatches: vi.fn(async () => list(1)) })
    await flushPromises()
    expect(ok.api().list.value?.watches[0].name).toBe('w1')
    expect(ok.api().loading.value).toBe(false)
    ok.w.unmount()
    const bad = host({ ListWatches: vi.fn(async () => { throw new Error('failed to open: no workspace') }) })
    await flushPromises()
    expect(bad.api().error.value).toBe('no workspace')
    expect(bad.api().loading.value).toBe(false)
    bad.w.unmount()
  })

  it('a burst of events makes one read, and a watch file change or a finished check each cause it', async () => {
    const ListWatches = vi.fn(async () => list(1))
    const { w } = host({ ListWatches })
    await flushPromises()
    expect(ListWatches).toHaveBeenCalledTimes(1)
    handlers['workspace:changed-on-disk']({ kind: 'watch' })
    handlers['watches:checked']()
    handlers['data:changed']()
    handlers['workspace:changed-on-disk']({ kind: 'watch_history' })
    await vi.advanceTimersByTimeAsync(400)
    expect(ListWatches).toHaveBeenCalledTimes(2)
    handlers['workspace:changed-on-disk']({ kind: 'watch_history' })
    await vi.advanceTimersByTimeAsync(400)
    expect(ListWatches).toHaveBeenCalledTimes(3)
    w.unmount()
  })

  it('a change to another kind of file does not read the list', async () => {
    const ListWatches = vi.fn(async () => list(1))
    const { w } = host({ ListWatches })
    await flushPromises()
    handlers['workspace:changed-on-disk']({ kind: 'notebook' })
    await vi.advanceTimersByTimeAsync(400)
    expect(ListWatches).toHaveBeenCalledTimes(1)
    w.unmount()
  })

  it('stops listening on unmount, and a read scheduled before it never happens', async () => {
    const ListWatches = vi.fn(async () => list(1))
    const { w } = host({ ListWatches })
    await flushPromises()
    handlers['watches:checked']()
    w.unmount()
    expect(offs.n).toBe(3)
    await vi.advanceTimersByTimeAsync(400)
    expect(ListWatches).toHaveBeenCalledTimes(1)
  })
})
