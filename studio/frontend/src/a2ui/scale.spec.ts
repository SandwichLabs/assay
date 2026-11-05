import { describe, it, expect } from 'vitest'
import { niceTicks, valueScale, frac, parseTime, timeTicks, timeAxis, timeFrac, maxTicksForWidth } from './scale'

const d = (s: string) => parseTime(s)!
const DAY = 24 * 3600 * 1000

describe('niceTicks', () => {
  it('uses 1/2/5 x 10^n steps and 3 to 6 ticks that cover the range', () => {
    for (const [lo, hi] of [[0, 100], [0, 87], [0, 1234], [3, 9], [0, 0.37], [12000, 98000], [0, 7]] as const) {
      const s = niceTicks(lo, hi)
      expect(s.ticks.length, `${lo}..${hi}`).toBeGreaterThanOrEqual(3)
      expect(s.ticks.length).toBeLessThanOrEqual(6)
      expect(s.min).toBeLessThanOrEqual(lo)
      expect(s.max).toBeGreaterThanOrEqual(hi)
      const mantissa = s.step / 10 ** Math.floor(Math.log10(s.step) + 1e-9)
      expect([1, 2, 5]).toContain(Math.round(mantissa * 1e6) / 1e6)
    }
  })
  it('gives clean values', () => {
    expect(niceTicks(0, 100).ticks).toEqual([0, 20, 40, 60, 80, 100])
    expect(niceTicks(0, 87).ticks).toEqual([0, 20, 40, 60, 80, 100])
    expect(niceTicks(0, 1234).ticks).toEqual([0, 500, 1000, 1500])
    expect(niceTicks(0, 10).ticks).toEqual([0, 2, 4, 6, 8, 10])
  })
  it('has no float noise', () => {
    const s = niceTicks(0, 0.37)
    for (const t of s.ticks) expect(String(t)).not.toMatch(/\d{8,}/)
    expect(s.ticks).toEqual([0, 0.1, 0.2, 0.3, 0.4])
  })
  it('handles negatives and spans through zero', () => {
    const s = niceTicks(-30, 80)
    expect(s.ticks).toContain(0)
    expect(s.min).toBeLessThanOrEqual(-30)
    expect(s.max).toBeGreaterThanOrEqual(80)
    expect(niceTicks(-100, -10).max).toBeGreaterThanOrEqual(-10)
  })
  it('opens a range around a single value', () => {
    const s = niceTicks(5, 5)
    expect(s.min).toBeLessThan(5)
    expect(s.max).toBeGreaterThan(5)
    const z = niceTicks(0, 0)
    expect(z.max).toBeGreaterThan(z.min)
  })
  it('survives non-finite input and swapped bounds', () => {
    expect(niceTicks(NaN, 3).ticks).toEqual([0, 1])
    expect(niceTicks(10, 0).ticks).toEqual(niceTicks(0, 10).ticks)
  })
})

describe('valueScale', () => {
  it('starts at zero for non-negative data', () => {
    const s = valueScale([201, 209, 205])
    expect(s.min).toBe(0)
    expect(s.max).toBeGreaterThanOrEqual(209)
  })
  it('ends at zero for non-positive data', () => {
    const s = valueScale([-5, -9])
    expect(s.max).toBe(0)
    expect(s.min).toBeLessThanOrEqual(-9)
  })
  it('fits the data when zero is false', () => {
    const s = valueScale([201, 209, 205], { zero: false })
    expect(s.min).toBeGreaterThan(0)
    expect(s.min).toBeLessThanOrEqual(201)
    expect(s.max).toBeGreaterThanOrEqual(209)
  })
  it('spans negative and positive data', () => {
    const s = valueScale([-40, 10, 25])
    expect(s.min).toBeLessThanOrEqual(-40)
    expect(s.max).toBeGreaterThanOrEqual(25)
    expect(s.ticks).toContain(0)
  })
  it('handles all-equal, all-zero, empty', () => {
    const eq = valueScale([7, 7, 7])
    expect(eq.min).toBe(0)
    expect(eq.max).toBeGreaterThanOrEqual(7)
    const eqFit = valueScale([7, 7, 7], { zero: false })
    expect(eqFit.min).toBeLessThan(7)
    expect(eqFit.max).toBeGreaterThan(7)
    expect(valueScale([0, 0]).max).toBeGreaterThan(0)
    expect(valueScale([]).ticks.length).toBeGreaterThan(1)
  })
  it('frac maps the domain to 0..1', () => {
    const s = valueScale([0, 100])
    expect(frac(s, s.min)).toBe(0)
    expect(frac(s, s.max)).toBe(1)
    expect(frac(s, 50)).toBeCloseTo(0.5)
  })
})

describe('parseTime', () => {
  it('reads dates, months and timestamps', () => {
    expect(parseTime('2025-04-03')).toBe(Date.UTC(2025, 3, 3))
    expect(parseTime('2025-04')).toBe(Date.UTC(2025, 3, 1))
    expect(parseTime('2025-04-03 10:30:15')).toBe(Date.UTC(2025, 3, 3, 10, 30, 15))
    expect(parseTime('2025-04-03T10:30:15.250Z')).toBe(Date.UTC(2025, 3, 3, 10, 30, 15))
    expect(parseTime('2025-04-03 10:30:00+02')).toBe(Date.UTC(2025, 3, 3, 10, 30))
  })
  it('rejects non-dates and impossible dates', () => {
    for (const s of ['north', '2025', '12', 'Q1 2025', '2025-13-01', '2025-02-30', '2025-04-03 25:00', '']) expect(parseTime(s), s).toBeNull()
  })
})

describe('timeAxis', () => {
  it('needs every label to parse and two distinct instants', () => {
    expect(timeAxis(['2025-01-01', 'x'])).toBeNull()
    expect(timeAxis(['2025-01-01'])).toBeNull()
    expect(timeAxis(['2025-01-01', '2025-01-01'])).toBeNull()
    expect(timeAxis([])).toBeNull()
    const a = timeAxis(['2025-01-01', '2025-01-02', '2025-01-10'])!
    expect(a.times).toHaveLength(3)
    expect(timeFrac(a, a.times[1])).toBeCloseTo(1 / 9)
  })
  it('spaces uneven dates by time, not by index', () => {
    const a = timeAxis(['2025-01-01', '2025-01-02', '2025-01-31'])!
    expect(timeFrac(a, a.times[1])).toBeCloseTo(1 / 30)
  })
})

describe('timeTicks', () => {
  const labels = (a: string, b: string, max = 6) => timeTicks(d(a), d(b), max).map((t) => t.label)
  it('daily span: day ticks', () => {
    expect(labels('2025-04-01', '2025-04-06')).toEqual(['Apr 1', 'Apr 2', 'Apr 3', 'Apr 4', 'Apr 5', 'Apr 6'])
  })
  it('a month or so: weekly ticks on Mondays', () => {
    const t = timeTicks(d('2025-04-01'), d('2025-05-10'), 6)
    expect(t.length).toBeLessThanOrEqual(6)
    expect(t.length).toBeGreaterThanOrEqual(2)
    for (const x of t) expect(new Date(x.t).getUTCDay()).toBe(1)
  })
  it("about half a year: month ticks labelled like Apr '25", () => {
    expect(labels('2025-01-01', '2025-06-01')).toEqual(["Jan '25", "Feb '25", "Mar '25", "Apr '25", "May '25", "Jun '25"])
  })
  it('a year with a small cap: quarters', () => {
    expect(labels('2025-01-01', '2025-12-01', 4)).toEqual(["Q1 '25", "Q2 '25", "Q3 '25", "Q4 '25"])
  })
  it('two years: calendar-aligned quarters or coarser', () => {
    const t = timeTicks(d('2024-01-01'), d('2025-12-01'), 6)
    expect(t.length).toBeLessThanOrEqual(6)
    for (const x of t) expect(new Date(x.t).getUTCMonth() % 3).toBe(0)
  })
  it('multi-year span: year ticks', () => {
    expect(labels('2019-06-15', '2025-03-01')).toEqual(['2020', '2021', '2022', '2023', '2024', '2025'])
    expect(labels('2000-01-01', '2025-03-01').length).toBeLessThanOrEqual(6)
  })
  it('days across a year boundary carry the year', () => {
    const l = labels('2024-12-30', '2025-01-03')
    expect(l[0]).toBe("Dec 30 '24")
    expect(l[l.length - 1]).toBe("Jan 3 '25")
  })
  it('ticks across a DST change are still 24h apart (wall-clock)', () => {
    const t = timeTicks(d('2025-03-08'), d('2025-03-12'), 6)
    expect(new Set(t.slice(1).map((x, i) => x.t - t[i].t))).toEqual(new Set([DAY]))
    const fall = timeTicks(d('2025-11-01'), d('2025-11-05'), 6)
    expect(new Set(fall.slice(1).map((x, i) => x.t - fall[i].t))).toEqual(new Set([DAY]))
  })
  it('intraday timestamps: hour ticks as HH:MM', () => {
    expect(labels('2025-04-03 08:00:00', '2025-04-03 13:00:00')).toEqual(['08:00', '09:00', '10:00', '11:00', '12:00', '13:00'])
  })
  it('hours across midnight carry the date', () => {
    const l = labels('2025-04-03 20:00:00', '2025-04-04 04:00:00', 5)
    expect(l.every((s) => /^[A-Z][a-z]{2} \d+ \d\d:\d\d$/.test(s))).toBe(true)
  })
  it('respects the tick cap and always returns at least the ends', () => {
    for (const cap of [2, 3, 4, 8]) expect(timeTicks(d('2025-01-01'), d('2025-03-20'), cap).length).toBeLessThanOrEqual(Math.max(cap, 2))
    expect(timeTicks(d('1500-01-01'), d('2500-01-01'), 3).length).toBeGreaterThanOrEqual(2)
  })
})

describe('maxTicksForWidth', () => {
  it('scales with width inside 3..8', () => {
    expect(maxTicksForWidth(0)).toBe(6)
    expect(maxTicksForWidth(150)).toBe(3)
    expect(maxTicksForWidth(2000)).toBe(8)
  })
})
