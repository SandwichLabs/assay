import { describe, expect, it } from 'vitest'
import { checkSummary, durationText, groupDecimal, readText, statusBadge, statusLabel, whenText } from './watchText'

describe('watchText', () => {
  it('writes a time as the local date and minute, or the fallback', () => {
    const d = new Date(2026, 10, 6, 9, 5)
    expect(whenText(d.toISOString())).toBe('2026-11-06 09:05')
    expect(whenText(undefined)).toBe('never')
    expect(whenText('nonsense', 'n/a')).toBe('n/a')
  })

  it('labels a status, with "not checked" before the first check', () => {
    expect(statusLabel('alert')).toBe('alert')
    expect(statusLabel('')).toBe('not checked')
    expect(statusLabel(undefined)).toBe('not checked')
    expect(statusBadge('alert')).toBe('badge--warn')
    expect(statusBadge('ok')).toBe('badge--success')
    expect(statusBadge('error')).toBe('badge--error')
    expect(statusBadge('')).toBe('badge--info')
  })

  it('says what a check read: the value, or the row count with its cap', () => {
    expect(readText({ value: 1240 })).toBe('1,240')
    expect(readText({ value: '123456789012345678901234567890' })).toBe('123,456,789,012,345,678,901,234,567,890')
    expect(readText({ value: '0.95' })).toBe('0.95')
    expect(readText({ rows: 1 })).toBe('1 row')
    expect(readText({ rows: 3 })).toBe('3 rows')
    expect(readText({ rows: 1001, rows_capped: true })).toBe('1,000+ rows')
    expect(readText({})).toBe('')
  })

  it('groups the digits of a plain decimal and leaves anything else alone', () => {
    expect(groupDecimal('1240')).toBe('1,240')
    expect(groupDecimal('-1234567.5')).toBe('-1,234,567.5')
    expect(groupDecimal('999')).toBe('999')
    expect(groupDecimal('1e9')).toBe('1e9')
  })

  it('labels a stopped or queued row', () => {
    expect(statusLabel('stopped')).toBe('stopped')
    expect(statusLabel('queued')).toBe('queued')
    expect(statusBadge('stopped')).toBe('badge--info')
  })

  it('summarises a run in one sentence', () => {
    const r = (status: WatchResult['status'], interrupted = false) => ({ status, interrupted })
    expect(checkSummary([r('ok'), r('alert'), r('alert'), r('error')], false)).toBe('Checked 4 watches: 2 fired, 1 error.')
    expect(checkSummary([r('ok'), r('ok')], false)).toBe('Checked 2 watches: all ok.')
    expect(checkSummary([r('ok')], false)).toBe('Checked 1 watch: all ok.')
    expect(checkSummary([r('ok'), r('error', true)], true)).toBe('Stopped. Checked 1 watch: all ok.')
    expect(checkSummary([r('error', true)], true)).toBe('Stopped. Checked 0 watches: nothing finished.')
  })

  it('writes a duration', () => {
    expect(durationText(12)).toBe('12 ms')
    expect(durationText(1400)).toBe('1.4 s')
  })
})
