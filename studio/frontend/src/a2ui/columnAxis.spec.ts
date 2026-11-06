import { describe, it, expect } from 'vitest'
import { buildColumnAxis, columnAt, columnTimes } from './columnAxis'

const days = (n: number, start = Date.UTC(2025, 3, 1), step = 1) =>
  Array.from({ length: n }, (_, i) => new Date(start + i * step * 86400000).toISOString().slice(0, 10))
const shown = (a: { labels: string[] }) => a.labels.filter(Boolean)
const even = (n: number, w: number) => Array.from({ length: n }, (_, i) => ((i + 0.5) / n) * w)

describe('columnTimes', () => {
  it('needs dates in strictly increasing order', () => {
    expect(columnTimes(days(3), true)).toHaveLength(3)
    expect(columnTimes(days(3).reverse(), true)).toBeNull()
    expect(columnTimes(['2025-04-01', '2025-04-01'], true)).toBeNull()
    expect(columnTimes(days(3), false)).toBeNull()
    expect(columnTimes(['2025-04-01'], true)).toBeNull()
    expect(columnTimes(['2025-04-01', 'north'], true)).toBeNull()
  })
  it('columnAt is the last column starting at or before a time', () => {
    const t = columnTimes(days(4, Date.UTC(2025, 3, 7), 7), true)!
    expect(columnAt(t, t[1] + 86400000)).toBe(1)
    expect(columnAt(t, t[3])).toBe(3)
    expect(columnAt(t, t[0] - 1)).toBe(0)
  })
})

describe('buildColumnAxis', () => {
  it('daily columns: day ticks under their own columns, never overlapping', () => {
    const l = days(60)
    const a = buildColumnAxis(l, true, even(60, 1440), 1440)
    expect(a.time).toBe(true)
    const idx = a.labels.map((x, i) => (x ? i : -1)).filter((i) => i >= 0)
    expect(idx.length).toBeGreaterThan(2)
    expect(idx.length).toBeLessThanOrEqual(8)
    expect(shown(a).every((x) => /^[A-Z][a-z]{2} \d{1,2}$/.test(x))).toBe(true)
    const mon = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun']
    for (const i of idx) {
      const d = new Date(l[i] + 'T00:00:00Z')
      expect(a.labels[i]).toBe(`${mon[d.getUTCMonth()]} ${d.getUTCDate()}`)
    }
    for (let k = 1; k < idx.length; k++) expect(idx[k] - idx[k - 1]).toBeGreaterThanOrEqual(2)
  })
  it('weekly columns: ticks land on the week column containing them', () => {
    const l = days(30, Date.UTC(2025, 0, 6), 7)
    const a = buildColumnAxis(l, true, even(30, 900), 900)
    expect(a.time).toBe(true)
    expect(shown(a).length).toBeGreaterThan(1)
  })
  it('monthly columns never get day-level labels', () => {
    const l = Array.from({ length: 14 }, (_, i) => `${2024 + Math.floor((i + 1) / 12)}-${String(((i + 1) % 12) + 1).padStart(2, '0')}`)
    const a = buildColumnAxis(l, true, even(14, 700), 700)
    expect(a.time).toBe(true)
    expect(shown(a).length).toBeGreaterThan(1)
    expect(shown(a).every((x) => /'2\d$|^\d{4}$/.test(x))).toBe(true)
  })
  it('year-crossing daily columns carry the year', () => {
    const a = buildColumnAxis(days(40, Date.UTC(2024, 11, 10)), true, even(40, 960), 960)
    expect(shown(a).some((x) => x.includes("'25") || x === '2025')).toBe(true)
  })
  it('unsorted, one column, mixed or out-of-order labels stay categories', () => {
    const l = days(10)
    expect(buildColumnAxis(l, false, [], 0)).toEqual({ time: false, labels: l })
    expect(buildColumnAxis(['2025-04-01'], true, [], 0).time).toBe(false)
    const mixed = [...l.slice(0, 4), 'north']
    expect(buildColumnAxis(mixed, true, [], 0)).toEqual({ time: false, labels: mixed })
    expect(buildColumnAxis(days(10).reverse(), true, [], 0).time).toBe(false)
  })
  it('works before the columns are measured', () => {
    const a = buildColumnAxis(days(30), true, [], 0)
    expect(a.time).toBe(true)
    expect(shown(a).length).toBeGreaterThan(0)
  })
  it('drops a label that would run past either end of the content', () => {
    const a = buildColumnAxis(['2025-04-01', '2025-04-02'], true, [10, 30], 40)
    expect(shown(a)).toEqual([])
  })
})
