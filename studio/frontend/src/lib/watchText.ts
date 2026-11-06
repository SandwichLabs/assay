// Text helpers of the Watches view: how a check's time, outcome and value read.

/** A recorded time as the local date and minute ("2026-11-06 14:52"), or the fallback when there is none. */
export function whenText(iso: string | undefined, none = 'never'): string {
  if (!iso) return none
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return none
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** The word a status badge shows: "alert", "ok", "error", "stopped", or "not checked" before the first check. */
export function statusLabel(status: string | undefined): string {
  return ['alert', 'ok', 'error', 'stopped', 'queued'].includes(status ?? '') ? (status as string) : 'not checked'
}

/** The badge class of a status (the shared SchoolHouse badge roles). */
export function statusBadge(status: string | undefined): string {
  return { alert: 'badge--warn', ok: 'badge--success', error: 'badge--error' }[status ?? ''] ?? 'badge--info'
}

/** A plain decimal with thousands separators ("1240" is "1,240", "-1234567.5" is "-1,234,567.5"); anything else as it is. */
export function groupDecimal(text: string): string {
  const m = /^(-?)(\d+)(\.\d+)?$/.exec(text)
  if (!m) return text
  return m[1] + m[2].replace(/\B(?=(\d{3})+$)/g, ',') + (m[3] ?? '')
}

/** What a check read: the value of a threshold check or the row count of a rows check, written as a reason writes it. */
export function readText(e: Pick<WatchEntry, 'value' | 'rows' | 'rows_capped'>): string {
  if (e.value !== undefined && e.value !== '') return groupDecimal(String(e.value))
  if (e.rows !== undefined) return e.rows_capped ? `${groupDecimal(String(e.rows - 1))}+ rows` : `${groupDecimal(String(e.rows))} ${e.rows === 1 ? 'row' : 'rows'}`
  return ''
}

/** A check's duration: "12 ms", "1.4 s". */
export function durationText(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}

/** The sentence announced when a check ends: "Checked 9 watches: 3 fired, 4 errors." */
export function checkSummary(results: Array<Pick<WatchResult, 'status' | 'interrupted'>>, cancelled: boolean): string {
  const done = results.filter((r) => !r.interrupted)
  const fired = done.filter((r) => r.status === 'alert').length
  const errors = done.filter((r) => r.status === 'error').length
  const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
  const parts = [fired ? `${fired} fired` : '', errors ? plural(errors, 'error', 'errors') : ''].filter(Boolean)
  const tail = parts.length ? parts.join(', ') : done.length ? 'all ok' : 'nothing finished'
  return `${cancelled ? 'Stopped. ' : ''}Checked ${plural(done.length, 'watch', 'watches')}: ${tail}.`
}

/** What a watch's newest check is: this window's result or the history's entry, whichever is not older. */
export interface Newest {
  /** The newest recorded or in-window outcome (never an interrupted one), if any. */
  entry?: WatchEntry
  /** Set only when this window's result is the newest: it alone carries the sample rows. */
  result?: WatchResult
  /** This window's check was cut short after the history's newest entry: shown as "stopped". */
  stopped: boolean
}

export function newest(w: Pick<WatchSummary, 'last'>, r?: WatchResult): Newest {
  const newer = !!r && (!w.last || !(Date.parse(r.at) < Date.parse(w.last.at)))
  if (r && newer && r.interrupted) return { entry: w.last, stopped: true }
  if (r && newer) return { entry: r, result: r, stopped: false }
  return { entry: w.last, stopped: false }
}
