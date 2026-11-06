// columnChart axis review fixes (#333): a label names its own column's date;
// narrow, short and wide scrolling charts. Pure function, real widths.
import { describe, it, expect } from 'vitest'
import { buildColumnAxis } from './columnAxis'

const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const ymd = (ms: number) => new Date(ms).toISOString().slice(0, 10)
const step = (n: number, start: string, days: number) => Array.from({ length: n }, (_, i) => ymd(Date.parse(start) + i * days * 86400000))
const days = (n: number) => step(n, '2025-04-01', 1)
const even = (n: number, w: number) => Array.from({ length: n }, (_, i) => ((i + 0.5) / n) * w)
const shown = (a: { labels: string[] }) => a.labels.filter(Boolean)

// a label names the column's own date when it is the day, month, quarter or year of it
function names(label: string, date: string): boolean {
  const d = new Date(date + 'T00:00:00Z')
  const y = d.getUTCFullYear()
  const yy = `'${String(y % 100).padStart(2, '0')}`
  return [
    `${MON[d.getUTCMonth()]} ${d.getUTCDate()}`,
    `${MON[d.getUTCMonth()]} ${d.getUTCDate()} ${yy}`,
    `${MON[d.getUTCMonth()]} ${yy}`,
    `Q${Math.floor(d.getUTCMonth() / 3) + 1} ${yy}`,
    String(y),
  ].includes(label)
}
const allNamed = (l: string[], a: { labels: string[] }) => a.labels.forEach((x, i) => x && expect(names(x, l[i]), `${l[i]} labelled ${x}`).toBe(true))

describe('labels name their own column', () => {
  it.each([800, 1200, 2000])('weekly Monday columns at %ipx', (w) => {
    const l = step(30, '2025-01-06', 7)
    const a = buildColumnAxis(l, true, even(30, w), w)
    allNamed(l, a)
    expect(a.labels[1]).not.toBe('Jan 14')
    expect(shown(a).length).toBeGreaterThan(1)
  })
  it.each([800, 1500])('weekly Sunday columns at %ipx never get Monday dates', (w) => {
    const l = step(30, '2025-01-05', 7)
    const a = buildColumnAxis(l, true, even(30, w), w)
    allNamed(l, a)
    expect(shown(a).length).toBeGreaterThan(1)
  })
  it('a missing month never puts a quarter label under the wrong month', () => {
    const l = ['2025-01-01', '2025-02-01', '2025-03-01', '2025-05-01', '2025-06-01', '2025-07-01', '2025-08-01']
    const a = buildColumnAxis(l, true, even(7, 700), 700)
    allNamed(l, a)
    expect(a.labels[2]).not.toBe("Q2 '25")
  })
  it('quarterly columns are labelled as quarters', () => {
    const l = ['2024-01-01', '2024-04-01', '2024-07-01', '2024-10-01', '2025-01-01', '2025-04-01']
    const a = buildColumnAxis(l, true, even(6, 600), 600)
    allNamed(l, a)
    expect(shown(a).some((x) => /^Q\d '2\d$/.test(x))).toBe(true)
  })
})

describe('narrow and short charts keep their labels', () => {
  it('7 daily columns at 180px', () => {
    const l = days(7)
    const a = buildColumnAxis(l, true, even(7, 180), 180)
    allNamed(l, a)
    expect(shown(a).length).toBeGreaterThanOrEqual(2)
    expect(a.labels[0]).toBe('Apr 1')
    expect(a.labels[6]).toBe('Apr 7')
    // day labels, never a year or month standing in for a day
    expect(shown(a).every((x) => /^[A-Z][a-z]{2} \d{1,2}$/.test(x))).toBe(true)
  })
  it('month-bucket labels name the bucket of their column, once each', () => {
    const l = step(30, '2025-01-05', 7)
    for (const w of [300, 450, 600]) {
      const a = buildColumnAxis(l, true, even(30, w), w)
      allNamed(l, a)
      const s = shown(a)
      expect(new Set(s).size).toBe(s.length)
    }
  })
  it('14 daily columns at 332px label both ends', () => {
    const l = days(14)
    const a = buildColumnAxis(l, true, even(14, 332), 332)
    allNamed(l, a)
    expect(a.labels[0]).not.toBe('')
    expect(a.labels[13]).not.toBe('')
  })
  it('2 weekly columns at 600px label both', () => {
    const a = buildColumnAxis(['2025-04-07', '2025-04-14'], true, even(2, 600), 600)
    expect(shown(a)).toEqual(['Apr 7', 'Apr 14'])
  })
  it('anchors the first and last label to the ends instead of dropping them', () => {
    const wide = buildColumnAxis(days(60), true, even(60, 1440), 1440)
    expect(wide.labels[0]).toBe('Apr 1')
    expect(wide.anchors[0]).toBe('start')
    expect(wide.anchors[59]).toBe('end')
    expect(wide.labels[59]).not.toBe('')
  })
  it('labels never overlap or leave the plot, whatever the width', () => {
    for (const [n, w] of [[7, 180], [14, 332], [30, 300], [60, 1440], [12, 240], [2, 600]] as const) {
      const a = buildColumnAxis(days(n), true, even(n, w), w)
      const iv = a.labels.flatMap((x, i) => {
        if (!x) return []
        const lw = x.length * 6.6
        const c = ((i + 0.5) / n) * w
        const left = a.anchors[i] === 'start' ? Math.max(0, c - w / n / 2) : a.anchors[i] === 'end' ? Math.min(w, c + w / n / 2) - lw : c - lw / 2
        return [[left, left + lw]]
      })
      for (let k = 1; k < iv.length; k++) expect(iv[k][0]).toBeGreaterThanOrEqual(iv[k - 1][1])
      for (const [l0, r0] of iv) {
        expect(l0).toBeGreaterThanOrEqual(-0.01)
        expect(r0).toBeLessThanOrEqual(w + 0.51)
      }
    }
  })
})

describe('wide scrolling charts', () => {
  it('365 daily columns over 8756px are labelled about every viewport width', () => {
    const l = days(365)
    const a = buildColumnAxis(l, true, even(365, 8756), 8756)
    allNamed(l, a)
    expect(shown(a).length).toBeGreaterThanOrEqual(20)
    expect(shown(a).length).toBeLessThanOrEqual(121)
  })
})
