import { describe, it, expect } from 'vitest'
import { buildXAxis, tickAnchor } from './xaxis'

describe('buildXAxis', () => {
  it('spaces categories evenly by index', () => {
    const a = buildXAxis(['north', 'south', 'east'], 600, 640)
    expect(a.time).toBe(false)
    expect(a.frac).toEqual([0, 0.5, 1])
    expect(a.ticks.map((t) => t.label)).toEqual(['north', 'south', 'east'])
  })
  it('centres a single point', () => {
    expect(buildXAxis(['only'], 600, 640).frac).toEqual([0.5])
  })
  it('spaces dates by time and sorts them', () => {
    const a = buildXAxis(['2025-01-31', '2025-01-01', '2025-01-02'], 600, 640)
    expect(a.time).toBe(true)
    expect(a.order).toEqual([1, 2, 0])
    expect(a.frac[0]).toBe(1)
    expect(a.frac[2]).toBeCloseTo(1 / 30)
  })
  it('daily, monthly and multi-year spans pick different tick units', () => {
    const daily = buildXAxis(['2025-04-01', '2025-04-02', '2025-04-03', '2025-04-04'], 600, 640)
    expect(daily.ticks.map((t) => t.label)).toEqual(['Apr 1', 'Apr 2', 'Apr 3', 'Apr 4'])
    const monthly = buildXAxis(['2025-01', '2025-02', '2025-03', '2025-04', '2025-05', '2025-06'], 600, 640)
    expect(monthly.ticks.map((t) => t.label)).toEqual(["Jan '25", "Feb '25", "Mar '25", "Apr '25", "May '25", "Jun '25"])
    const years = buildXAxis(['2019-01-01', '2021-06-01', '2025-01-01'], 600, 640)
    expect(years.ticks.map((t) => t.label)).toContain('2024')
  })
  it('does not tick finer than the labels', () => {
    expect(buildXAxis(['2025-01', '2025-02'], 600, 640).ticks.map((t) => t.label)).toEqual(["Jan '25", "Feb '25"])
    expect(buildXAxis(['2025-04-03', '2025-04-04'], 600, 640).ticks.map((t) => t.label)).toEqual(['Apr 3', 'Apr 4'])
  })
  it('falls back to categories when one label is not a date', () => {
    expect(buildXAxis(['2025-01-01', 'total'], 600, 640).time).toBe(false)
  })
  it('handles no labels', () => {
    const a = buildXAxis([], 600, 640)
    expect(a.ticks).toEqual([])
    expect(a.order).toEqual([])
  })
})

describe('tickAnchor', () => {
  it('anchors the ends inward and centres the middle', () => {
    expect(tickAnchor(0).transform).toBe('translateX(0)')
    expect(tickAnchor(1).transform).toBe('translateX(-100%)')
    expect(tickAnchor(0.5).transform).toBe('translateX(-50%)')
  })
})
