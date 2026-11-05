// useBulkAccept (E4, #321 3c): "Accept all" reads every open proposal, lists
// them, and sends each one's shown_rev, never overwriting; a refusal stops the
// run and is reported with what was accepted before it.
type MockApp = Partial<Record<keyof AppApi, any>>
import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import { useBulkAccept } from './useBulkAccept'

const prop = (id: string, kind: Proposal['kind'], name: string): Proposal => ({
  id, kind, name, status: 'open', proposed_at: '2026-10-29T15:30:12Z', proposed_by: 'agent',
  rationale: 'why', evidence: [], replaces_rev: '', decided_at: '',
})
const pv = (p: Proposal, stale = false): ProposalPreview => ({
  proposal: p, target: 'semantics/x', exists: stale, current_text: '', current_rev: '', proposed_text: 'new\n',
  stale, shown_rev: 'pin-' + p.id, warnings: [],
})

function install(app: MockApp) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

const A = prop('2026-b', 'metric', 'orders')
const B = prop('2026-a', 'glossary', 'Churn')

describe('useBulkAccept', () => {
  it('reads each proposal oldest first, lists them, then sends the shown revs', async () => {
    const app: MockApp = {
      GetProposal: vi.fn(async (id: string) => pv(id === A.id ? A : B)),
      AcceptProposals: vi.fn(async () => ({ accepted: [
        { id: B.id, kind: 'glossary', name: 'Churn', target: 'semantics/glossary.md' },
        { id: A.id, kind: 'metric', name: 'orders', target: 'semantics/metrics/orders.yaml' },
      ] })),
    }
    install(app)
    const done = vi.fn(async () => {})
    const b = useBulkAccept(ref([A, B]), done)
    await b.prepare()
    expect(b.confirming.value).toBe(true)
    expect(b.items.value.map((i) => i.id)).toEqual([B.id, A.id])
    expect(b.items.value[0]).toMatchObject({ target: 'semantics/x', added: 1, removed: 0 })
    expect(app.AcceptProposals).not.toHaveBeenCalled()

    await b.confirm()
    expect(app.AcceptProposals).toHaveBeenCalledWith([
      { id: B.id, shown_rev: 'pin-' + B.id },
      { id: A.id, shown_rev: 'pin-' + A.id },
    ])
    expect(b.summary.value).toBe('Accepted 2: glossary Churn, metric orders.')
    expect(b.failed.value).toBe(false)
    expect(done).toHaveBeenCalledOnce()
    expect(b.confirming.value).toBe(false)
  })

  it('cancel writes nothing', async () => {
    const app: MockApp = { GetProposal: vi.fn(async () => pv(A)), AcceptProposals: vi.fn() }
    install(app)
    const b = useBulkAccept(ref([A]), async () => {})
    await b.prepare()
    b.cancel()
    expect(b.confirming.value).toBe(false)
    expect(app.AcceptProposals).not.toHaveBeenCalled()
  })

  it('a stale proposal is flagged in the list, and a refusal is reported with what came before', async () => {
    const app: MockApp = {
      GetProposal: vi.fn(async (id: string) => pv(id === A.id ? A : B, id === A.id)),
      AcceptProposals: vi.fn(async () => ({
        accepted: [{ id: B.id, kind: 'glossary', name: 'Churn', target: 'semantics/glossary.md' }],
        failed_id: A.id, error: 'semantics/metrics/orders.yaml changed since this was proposed',
      })),
    }
    install(app)
    const b = useBulkAccept(ref([A, B]), async () => {})
    await b.prepare()
    expect(b.items.value.find((i) => i.id === A.id)?.stale).toBe(true)
    await b.confirm()
    expect(b.failed.value).toBe(true)
    expect(b.summary.value).toBe('Accepted 1: glossary Churn. Stopped at metric orders: semantics/metrics/orders.yaml changed since this was proposed. It and the ones after it are still open.')
  })

  it('a proposal that cannot be read blocks the dialog and says why', async () => {
    install({ GetProposal: vi.fn(async () => { throw new Error('glossary.md has an unclosed fence') }), AcceptProposals: vi.fn() })
    const b = useBulkAccept(ref([A]), async () => {})
    await b.prepare()
    expect(b.confirming.value).toBe(false)
    expect(b.error.value).toContain('unclosed fence')
  })

  it('leaves out a proposal decided elsewhere', async () => {
    install({ GetProposal: vi.fn(async (id: string) => ({ ...pv(id === A.id ? A : B), proposal: { ...(id === A.id ? A : B), status: id === A.id ? 'rejected' : 'open' } })), AcceptProposals: vi.fn() })
    const b = useBulkAccept(ref([A, B]), async () => {})
    await b.prepare()
    expect(b.items.value.map((i) => i.id)).toEqual([B.id])
  })

  it('a file written but not marked is not reported as nothing accepted', async () => {
    install({
      GetProposal: vi.fn(async () => pv(A)),
      AcceptProposals: vi.fn(async () => ({ accepted: [], failed_id: A.id, error: 'x was written, but proposal y could not be marked accepted', written_not_marked: true })),
    })
    const b = useBulkAccept(ref([A]), async () => {})
    await b.prepare()
    await b.confirm()
    expect(b.summary.value).not.toContain('Nothing was accepted')
    expect(b.summary.value).toContain('file was written')
  })
})
