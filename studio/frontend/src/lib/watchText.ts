// Text helpers of the Watches view: how a check's time, outcome and value read.

/** A recorded time as the local date and minute ("2026-11-06 14:52"), or the fallback when there is none. */
export function whenText(iso: string | undefined, none = 'never'): string {
  if (!iso) return none
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return none
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

/** The word a status badge shows: "alert", "ok", "error", or "not checked" before the first check. */
export function statusLabel(status: string | undefined): string {
  return status === 'alert' || status === 'ok' || status === 'error' ? status : 'not checked'
}

/** The badge class of a status (the shared SchoolHouse badge roles). */
export function statusBadge(status: string | undefined): string {
  return { alert: 'badge--warn', ok: 'badge--success', error: 'badge--error' }[status ?? ''] ?? 'badge--info'
}

/** What a check read: the value of a threshold check or the row count of a rows check. */
export function readText(e: Pick<WatchEntry, 'value' | 'rows' | 'rows_capped'>): string {
  if (e.value !== undefined && e.value !== '') return String(e.value)
  if (e.rows !== undefined) return e.rows_capped ? `${e.rows - 1}+ rows` : `${e.rows} ${e.rows === 1 ? 'row' : 'rows'}`
  return ''
}

/** A check's duration: "12 ms", "1.4 s". */
export function durationText(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}
