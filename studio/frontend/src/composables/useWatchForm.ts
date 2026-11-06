// useWatchForm — the New watch / Edit form (E7 W2). It builds a WatchDraft from
// plain inputs (a name, a saved query or a metric with the query's declared
// parameters, a condition) and saves it through SaveWatch. It re-implements no
// rule of the file format: the YAML preview and every refusal come from Go's own
// decoder (PreviewWatch / SaveWatch), and the form only insists on the fields
// that cannot be empty. The file is the source of truth: editing loads the revision on
// disk and saves against it, so a change made meanwhile (an editor, git) is the
// ordinary conflict banner (useConflict) with "Reload from disk" and "Keep mine".
import { computed, ref, watch, type UnwrapNestedRefs } from 'vue'
import { App } from '../lib/bindings'
import { errorText, isConflict } from '../lib/errors'
import { useConflict } from './useConflict'

export type CondKind = 'any' | 'none' | 'above' | 'below' | 'outside'
export type QuestionKind = 'query' | 'metric'

const PREVIEW_DELAY_MS = 200
const PLACEHOLDER_NAME = 'new_watch' // the preview of a form whose name is still empty

export function useWatchForm(onSaved: (name: string) => void | Promise<void>) {
  const open = ref(false)
  const mode = ref<'new' | 'edit'>('new')
  const loading = ref(false)
  const busy = ref(false)
  const name = ref('')
  const description = ref('')
  const kind = ref<QuestionKind>('query')
  const target = ref('')
  const params = ref<Record<string, string>>({})
  const cond = ref<CondKind>('any')
  const a = ref('')
  const b = ref('')
  const rev = ref('')
  // The file on disk could not be read: the form starts empty and saving replaces it, against
  // the revision of those bytes (a file changed since is the usual conflict).
  const replacing = ref('')
  const queries = ref<QuerySummary[]>([])
  const metrics = ref<DefinitionSummary[]>([])
  const specs = ref<QueryParamT[]>([])
  const preview = ref('')
  const previewError = ref('')
  const error = ref('')

  const conflict = useConflict({
    currentId: () => (mode.value === 'edit' ? name.value : ''),
    fetchRev: async (n) => (await App.GetWatch(n)).rev,
  })

  const when = computed<WatchWhen>(() => {
    switch (cond.value) {
      case 'any': return { rows: 'any' }
      case 'none': return { rows: 'none' }
      case 'above': return { above: a.value.trim() }
      case 'below': return { below: a.value.trim() }
      default: return { outside: [a.value.trim(), b.value.trim()] }
    }
  })

  const draft = computed<WatchDraft>(() => ({
    name: name.value.trim(),
    description: description.value.trim() || undefined,
    question: kind.value === 'query' ? { query: target.value, params: { ...params.value } } : { metric: target.value },
    when: when.value,
    rev: rev.value,
  }))

  /** The names of the parameter inputs: the query's declared ones, then any the file names that it does not declare. */
  const paramNames = computed(() => {
    if (kind.value !== 'query') return []
    return [...new Set([...specs.value.map((p) => p.name), ...Object.keys(params.value)])]
  })
  const specOf = (n: string) => specs.value.find((p) => p.name === n)

  /** The saved query or metric the file names is not in the workspace (deleted, renamed, or unreadable). */
  const targetMissing = computed(() => {
    if (loading.value || !target.value) return false
    return kind.value === 'query' ? !queries.value.some((q) => q.id === target.value) : !metrics.value.some((m) => m.name === target.value)
  })
  /** A declared parameter with no default must be given a value: the check would refuse it. */
  const requiredParam = (n: string) => {
    const p = specOf(n)
    return !!p && p.required && !p.default
  }
  const paramMissing = computed(() => paramNames.value.some((n) => requiredParam(n) && !(params.value[n] ?? '').trim()))

  // Only the fields that cannot be empty; everything else is Go's to refuse.
  const canSave = computed(() => {
    if (!name.value.trim() || !target.value || targetMissing.value || paramMissing.value) return false
    if (cond.value === 'above' || cond.value === 'below') return a.value.trim() !== ''
    if (cond.value === 'outside') return a.value.trim() !== '' && b.value.trim() !== ''
    return true
  })

  let previewTimer: ReturnType<typeof setTimeout> | undefined
  let previewSeq = 0
  async function refreshPreview() {
    const seq = ++previewSeq
    try {
      const text = await App.PreviewWatch({ ...draft.value, name: draft.value.name || PLACEHOLDER_NAME })
      if (seq === previewSeq) { preview.value = text; previewError.value = '' }
    } catch (e) {
      if (seq === previewSeq) { preview.value = ''; previewError.value = errorText(e) }
    }
  }
  watch(draft, () => {
    if (!open.value || loading.value) return
    if (previewTimer) clearTimeout(previewTimer)
    previewTimer = setTimeout(() => { previewTimer = undefined; void refreshPreview() }, PREVIEW_DELAY_MS)
  }, { deep: true })

  async function loadOptions() {
    const [q, defs] = await Promise.allSettled([App.ListQueries(), App.ListDefinitions()])
    queries.value = q.status === 'fulfilled' ? q.value : []
    metrics.value = defs.status === 'fulfilled' ? defs.value.metrics.filter((m) => !m.error) : []
  }

  async function loadSpecs() {
    specs.value = []
    if (kind.value !== 'query' || !target.value) return
    const id = target.value
    try {
      const q = await App.GetQuery(id)
      if (kind.value === 'query' && target.value === id) specs.value = q.parameters ?? []
    } catch {
      // a query that is gone: the inputs of the params the file names remain
    }
  }
  watch([kind, target], () => { void loadSpecs() })

  /** Picking another question starts its parameters afresh; the file's own are kept while it loads. */
  function pickKind(k: QuestionKind) {
    if (kind.value === k) return
    kind.value = k
    target.value = ''
    params.value = {}
  }
  function pickTarget(t: string) {
    if (target.value === t) return
    target.value = t
    params.value = {}
  }
  function setParam(n: string, v: string) { params.value = { ...params.value, [n]: v } }

  function reset() {
    name.value = ''; description.value = ''; kind.value = 'query'; target.value = ''; params.value = {}
    cond.value = 'any'; a.value = ''; b.value = ''; rev.value = ''; replacing.value = ''
    specs.value = []; preview.value = ''; previewError.value = ''; error.value = ''
    conflict.clear()
  }

  function fill(d: WatchDetail) {
    name.value = d.name
    rev.value = d.rev ?? ''
    if (d.error) {
      replacing.value = d.error // the form starts empty; saving writes a new file over the unreadable one
      return
    }
    description.value = d.description ?? ''
    kind.value = d.question.metric ? 'metric' : 'query'
    target.value = d.question.metric || d.question.query || ''
    params.value = { ...(d.question.params ?? {}) }
    const w = d.when
    if (w.rows === 'none') cond.value = 'none'
    else if (w.rows === 'any') cond.value = 'any'
    else if (w.above !== undefined && w.above !== '') { cond.value = 'above'; a.value = w.above }
    else if (w.below !== undefined && w.below !== '') { cond.value = 'below'; a.value = w.below }
    else if (w.outside?.length) { cond.value = 'outside'; a.value = w.outside[0] ?? ''; b.value = w.outside[1] ?? '' }
  }

  async function openNew() {
    reset()
    mode.value = 'new'
    open.value = true
    loading.value = true
    await loadOptions()
    loading.value = false
    void refreshPreview()
  }

  async function openEdit(n: string) {
    reset()
    mode.value = 'edit'
    open.value = true
    loading.value = true
    try {
      await loadOptions()
      fill(await App.GetWatch(n))
      await loadSpecs()
    } catch (e) {
      error.value = errorText(e)
    }
    loading.value = false
    void refreshPreview()
  }

  function close() {
    if (busy.value || conflict.busy.value) return
    if (previewTimer) { clearTimeout(previewTimer); previewTimer = undefined }
    open.value = false
  }

  async function save(overwrite = false) {
    if (!canSave.value || busy.value) return
    busy.value = true
    error.value = ''
    try {
      const saved = await App.SaveWatch({ ...draft.value, overwrite })
      rev.value = saved
      conflict.clear()
      open.value = false
      await onSaved(draft.value.name)
    } catch (e) {
      if (isConflict(e) && mode.value === 'edit') conflict.raise(/deleted/.test(errorText(e)))
      else if (isConflict(e)) error.value = `A watch named ${draft.value.name} already exists. Pick another name, or edit that one.`
      else error.value = errorText(e)
    }
    busy.value = false
  }

  /** "Reload from disk": drop the edits and show the file as it is now. */
  async function reloadFromDisk() {
    await conflict.resolving(async () => {
      if (conflict.conflict.value?.deleted) { // the file is gone: there is nothing to show
        conflict.clear()
        open.value = false
        await onSaved(name.value)
        return
      }
      const d = await App.GetWatch(name.value)
      params.value = {}; a.value = ''; b.value = ''; replacing.value = ''
      fill(d)
      await loadSpecs()
      conflict.clear()
      void refreshPreview()
    }, (t) => { error.value = t })
  }

  /** "Keep mine": write the form over the file that changed. */
  async function keepMine() {
    await conflict.resolving(async () => {
      const saved = await App.SaveWatch({ ...draft.value, overwrite: true })
      rev.value = saved
      conflict.clear()
      open.value = false
      await onSaved(draft.value.name)
    }, (t) => { error.value = t })
  }

  return {
    open, mode, loading, busy, name, description, kind, target, params, cond, a, b, rev, replacing,
    queries, metrics, specs, paramNames, specOf, requiredParam, targetMissing, preview, previewError, error, conflict, canSave, draft,
    openNew, openEdit, close, save, pickKind, pickTarget, setParam, reloadFromDisk, keepMine,
  }
}

/** The form as the dialog gets it (wrap in reactive() so the refs read as values). */
export type WatchForm = UnwrapNestedRefs<ReturnType<typeof useWatchForm>>
