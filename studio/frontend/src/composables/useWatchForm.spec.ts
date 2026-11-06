// useWatchForm (E7 W2): the form builds a draft from plain inputs and leaves
// every rule to Go (the preview and every refusal are the decoder's). Saving is
// against the loaded revision: a name that exists is an inline message, an
// edited file that changed is the conflict banner with Reload and Keep mine.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, expect, it, vi } from 'vitest'
import { useWatchForm } from './useWatchForm'

const QUERIES: QuerySummary[] = [{ id: 'stuck_orders', title: 'Stuck orders', engine: 'duckdb' }, { id: 'order_count', title: 'Order count', engine: 'duckdb' }]
const METRICS: DefinitionSummary[] = [{ kind: 'metric', name: 'revenue', title: 'Revenue' }, { kind: 'metric', name: 'bad', error: 'x' }]
const SPECS: QueryParamT[] = [{ name: 'days', type: 'integer', required: false, default: '1' }]

const detail = (over: Partial<WatchDetail> = {}): WatchDetail => ({
  name: 'stuck', rev: 'r1', question: { query: 'stuck_orders', params: { days: '3' } }, when: { above: '5' }, text: '', ...over,
})

function makeApp(over: MockApp = {}) {
  const app: MockApp = {
    ListQueries: vi.fn(async () => QUERIES),
    ListDefinitions: vi.fn(async () => ({ entities: [], metrics: METRICS, joins: [], terms: [], open_proposals: 0 })),
    GetQuery: vi.fn(async (id: string) => ({ id, parameters: id === 'stuck_orders' ? SPECS : [] })),
    PreviewWatch: vi.fn(async () => 'question:\n  query: stuck_orders\n'),
    SaveWatch: vi.fn(async () => 'r2'),
    GetWatch: vi.fn(async () => detail()),
    ...over,
  }
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
  return app
}

describe('useWatchForm', () => {
  it('lists saved queries and valid metrics, and takes the parameters a query declares', async () => {
    const app = makeApp()
    const f = useWatchForm(vi.fn())
    await f.openNew()
    expect(f.queries.value.map((q) => q.id)).toEqual(['stuck_orders', 'order_count'])
    expect(f.metrics.value.map((m) => m.name)).toEqual(['revenue'])
    f.pickTarget('stuck_orders')
    await vi.waitFor(() => expect(f.paramNames.value).toEqual(['days']))
    expect(app.GetQuery).toHaveBeenCalledWith('stuck_orders')
    f.setParam('days', '2')
    f.name.value = ' stuck '
    f.cond.value = 'outside'
    f.a.value = '10'
    f.b.value = ' 90'
    expect(f.draft.value).toEqual({
      name: 'stuck', description: undefined, question: { query: 'stuck_orders', params: { days: '2' } }, when: { outside: ['10', '90'] }, rev: '',
    })
  })

  it('a metric question has no parameters; picking another question starts them afresh', async () => {
    makeApp()
    const f = useWatchForm(vi.fn())
    await f.openNew()
    f.pickTarget('stuck_orders')
    f.setParam('days', '2')
    f.pickKind('metric')
    f.pickTarget('revenue')
    f.name.value = 'rev'
    f.cond.value = 'below'
    f.a.value = '0.95'
    expect(f.paramNames.value).toEqual([])
    expect(f.draft.value.question).toEqual({ metric: 'revenue' })
    expect(f.draft.value.when).toEqual({ below: '0.95' })
  })

  it('insists only on the fields that cannot be empty', async () => {
    makeApp()
    const f = useWatchForm(vi.fn())
    await f.openNew()
    expect(f.canSave.value).toBe(false)
    f.name.value = 'w'
    f.pickTarget('order_count')
    expect(f.canSave.value).toBe(true) // rows: any
    f.cond.value = 'above'
    expect(f.canSave.value).toBe(false)
    f.a.value = '1'
    expect(f.canSave.value).toBe(true)
    f.cond.value = 'outside'
    expect(f.canSave.value).toBe(false)
    f.b.value = '2'
    expect(f.canSave.value).toBe(true)
  })

  it('previews through Go and shows its refusal as the reason, with a placeholder name while empty', async () => {
    vi.useFakeTimers()
    try {
      const preview = vi.fn().mockResolvedValueOnce('yaml\n').mockRejectedValueOnce(new Error('failed to x: a threshold must be a plain decimal number'))
      makeApp({ PreviewWatch: preview })
      const f = useWatchForm(vi.fn())
      await f.openNew()
      expect(preview).toHaveBeenLastCalledWith(expect.objectContaining({ name: 'new_watch' }))
      expect(f.preview.value).toBe('yaml\n')
      f.pickTarget('order_count')
      await vi.advanceTimersByTimeAsync(300)
      expect(f.previewError.value).toBe('a threshold must be a plain decimal number')
      expect(f.preview.value).toBe('')
    } finally {
      vi.useRealTimers()
    }
  })

  it('saves a new watch against no revision and reports it saved', async () => {
    const app = makeApp()
    const saved = vi.fn()
    const f = useWatchForm(saved)
    await f.openNew()
    f.name.value = 'w'
    f.pickTarget('order_count')
    await f.save()
    expect(app.SaveWatch).toHaveBeenCalledWith(expect.objectContaining({ name: 'w', rev: '', overwrite: false }))
    expect(saved).toHaveBeenCalledWith('w')
    expect(f.open.value).toBe(false)
  })

  it('a name that exists is an inline message, not the banner', async () => {
    makeApp({ SaveWatch: vi.fn().mockRejectedValue(new Error('conflict: the file changed on disk since it was loaded')) })
    const f = useWatchForm(vi.fn())
    await f.openNew()
    f.name.value = 'w'
    f.pickTarget('order_count')
    await f.save()
    expect(f.error.value).toContain('A watch named w already exists')
    expect(f.conflict.conflict.value).toBeNull()
    expect(f.open.value).toBe(true)
  })

  it('a validation error from Go stays in the form', async () => {
    makeApp({ SaveWatch: vi.fn().mockRejectedValue(new Error('watches/w.yaml: when: choose exactly one condition')) })
    const f = useWatchForm(vi.fn())
    await f.openNew()
    f.name.value = 'w'
    f.pickTarget('order_count')
    await f.save()
    expect(f.error.value).toBe('watches/w.yaml: when: choose exactly one condition')
    expect(f.open.value).toBe(true)
  })

  it('opens an edit from the file, keeps its revision and saves against it', async () => {
    const app = makeApp()
    const f = useWatchForm(vi.fn())
    await f.openEdit('stuck')
    expect(f.name.value).toBe('stuck')
    expect(f.target.value).toBe('stuck_orders')
    expect(f.params.value).toEqual({ days: '3' })
    expect(f.cond.value).toBe('above')
    expect(f.a.value).toBe('5')
    await f.save()
    expect(app.SaveWatch).toHaveBeenCalledWith(expect.objectContaining({ name: 'stuck', rev: 'r1', overwrite: false }))
  })

  it('a file changed meanwhile raises the banner; Keep mine overwrites, Reload shows the file', async () => {
    const save = vi.fn().mockRejectedValueOnce(new Error('conflict: watches/stuck.yaml changed'))
    const get = vi.fn().mockResolvedValueOnce(detail()).mockResolvedValue(detail({ rev: 'r9', when: { below: '2' } }))
    const saved = vi.fn()
    makeApp({ SaveWatch: save, GetWatch: get })
    const f = useWatchForm(saved)
    await f.openEdit('stuck')
    await f.save()
    expect(f.conflict.conflict.value).toEqual({ deleted: false })
    expect(f.open.value).toBe(true)
    await f.reloadFromDisk()
    expect(f.conflict.conflict.value).toBeNull()
    expect(f.cond.value).toBe('below')
    expect(f.a.value).toBe('2')
    expect(f.rev.value).toBe('r9')

    save.mockRejectedValueOnce(new Error('conflict: changed again')).mockResolvedValueOnce('r3')
    await f.save()
    expect(f.conflict.conflict.value).not.toBeNull()
    await f.keepMine()
    expect(save).toHaveBeenLastCalledWith(expect.objectContaining({ overwrite: true }))
    expect(saved).toHaveBeenCalledWith('stuck')
    expect(f.open.value).toBe(false)
  })

  it('a deleted file: Close it closes the form', async () => {
    const saved = vi.fn()
    makeApp({ SaveWatch: vi.fn().mockRejectedValue(new Error('conflict: watches/stuck.yaml was deleted on disk')) })
    const f = useWatchForm(saved)
    await f.openEdit('stuck')
    await f.save()
    expect(f.conflict.conflict.value).toEqual({ deleted: true })
    await f.reloadFromDisk()
    expect(f.open.value).toBe(false)
    expect(saved).toHaveBeenCalled()
  })

  it('an unreadable file opens empty and saving replaces it', async () => {
    const app = makeApp({ GetWatch: vi.fn(async () => detail({ error: 'line 3: unknown field', question: {}, when: {} })) })
    const f = useWatchForm(vi.fn())
    await f.openEdit('stuck')
    expect(f.replacing.value).toContain('unknown field')
    f.pickTarget('order_count')
    await f.save()
    expect(app.SaveWatch).toHaveBeenCalledWith(expect.objectContaining({ name: 'stuck', overwrite: true }))
  })
})
