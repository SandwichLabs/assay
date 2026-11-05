// useBulkAccept (E4, #321 3c): "Accept all" for the proposals inbox. It reads
// every open proposal first (GetProposal), lists them in a confirmation, and on
// confirm sends the list with each shown_rev, the pin of what was read, to one
// binding that accepts them in order and never overwrites. The run stops at the
// first proposal Go refuses (the file changed, the proposal changed again,
// invalid); what was accepted before it stays accepted. The outcome stays on
// screen as text until the next run or dismissal.
import { computed, ref, type Ref } from 'vue'
import { App } from '../lib/bindings'
import { errorText } from '../lib/errors'

export interface BulkItem {
  id: string
  kind: Proposal['kind']
  name: string
  shownRev: string
  stale: boolean
}

export function useBulkAccept(open: Ref<Proposal[]>, onDone: () => Promise<void>) {
  const items = ref<BulkItem[]>([])
  const confirming = ref(false)
  const busy = ref(false)
  const error = ref('')
  const outcome = ref<{ accepted: string[]; stopped: string; reason: string } | null>(null)

  const label = (i: { kind: string; name: string }) => `${i.kind} ${i.name}`
  // The confirmation text: one line per proposal, oldest first (the order it runs in).
  const message = computed(() => {
    const lines = items.value.map((i) => `• ${label(i)}${i.stale ? ' (changed since proposed: this stops the run)' : ''}`)
    return `These ${items.value.length} proposals are written exactly as you reviewed them, in this order. Nothing is overwritten; the run stops at the first one that cannot be written.\n\n${lines.join('\n')}`
  })

  /** Read each open proposal, then ask. Nothing is written until confirm(). */
  async function prepare() {
    error.value = ''
    outcome.value = null
    try {
      const ordered = [...open.value].sort((a, b) => (a.id < b.id ? -1 : 1)) // oldest first
      const previews = await Promise.all(ordered.map((p) => App.GetProposal(p.id)))
      items.value = previews.map((pv) => ({ id: pv.proposal.id, kind: pv.proposal.kind, name: pv.proposal.name, shownRev: pv.shown_rev, stale: pv.stale }))
      confirming.value = items.value.length > 0
    } catch (e) {
      items.value = []
      error.value = errorText(e)
    }
  }

  function cancel() {
    confirming.value = false
    items.value = []
  }

  async function confirm() {
    const run = items.value
    confirming.value = false
    busy.value = true
    error.value = ''
    try {
      const res = await App.AcceptProposals(run.map((i) => ({ id: i.id, shown_rev: i.shownRev })))
      const byId = new Map(run.map((i) => [i.id, i]))
      const stopped = res.failed_id ? label(byId.get(res.failed_id) ?? { kind: 'proposal', name: res.failed_id }) : ''
      outcome.value = { accepted: res.accepted.map((a) => label(a)), stopped, reason: res.error ?? '' }
    } catch (e) {
      error.value = errorText(e)
    } finally {
      items.value = []
      busy.value = false
      await onDone()
    }
  }

  /** One sentence for the result: what was written and what stopped the run. */
  const summary = computed(() => {
    const o = outcome.value
    if (!o) return ''
    const done = o.accepted.length ? `Accepted ${o.accepted.length}: ${o.accepted.join(', ')}.` : 'Nothing was accepted.'
    return o.stopped ? `${done} Stopped at ${o.stopped}: ${o.reason}. It and the ones after it are still open.` : done
  })
  const failed = computed(() => !!outcome.value?.stopped)
  const dismiss = () => { outcome.value = null }

  return { items, confirming, busy, error, message, summary, failed, prepare, cancel, confirm, dismiss }
}
