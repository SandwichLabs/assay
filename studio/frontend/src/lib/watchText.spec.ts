import { describe, expect, it } from 'vitest'
import { durationText, readText, statusBadge, statusLabel, whenText } from './watchText'

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
    expect(readText({ value: 1240 })).toBe('1240')
    expect(readText({ value: '123456789012345678901234567890' })).toBe('123456789012345678901234567890')
    expect(readText({ rows: 1 })).toBe('1 row')
    expect(readText({ rows: 3 })).toBe('3 rows')
    expect(readText({ rows: 1001, rows_capped: true })).toBe('1000+ rows')
    expect(readText({})).toBe('')
  })

  it('writes a duration', () => {
    expect(durationText(12)).toBe('12 ms')
    expect(durationText(1400)).toBe('1.4 s')
  })
})
