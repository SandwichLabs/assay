import { describe, it, expect } from 'vitest'
import { foldSeries, fromInlineSeries, fromPoints, isSeriesShape, toMultiSeries } from './multiseries'

const names = (m: { series: { name: string }[] }) => m.series.map((s) => s.name)

describe('toMultiSeries: shape detection', () => {
  it('reads a (label, series, value) result as long', () => {
    const m = toMultiSeries({
      columns: ['month', 'rep', 'revenue'],
      rows: [['2025-01', 'Ana', 10], ['2025-01', 'Ben', 7], ['2025-02', 'Ana', 12], ['2025-02', 'Ben', 9]],
    })
    expect(m.shape).toBe('long')
    expect(m.labels).toEqual(['2025-01', '2025-02'])
    expect(m.series).toEqual([{ name: 'Ana', values: [10, 12] }, { name: 'Ben', values: [7, 9] }])
  })

  it('reads a label then several numeric columns as wide', () => {
    const m = toMultiSeries({ columns: ['month', 'cost', 'revenue'], rows: [['Jan', 1, 5], ['Feb', 2, 6]] })
    expect(m.shape).toBe('wide')
    expect(m.series).toEqual([{ name: 'cost', values: [1, 2] }, { name: 'revenue', values: [5, 6] }])
  })

  it('keeps a two-column result a single series, with the legacy label/value rules', () => {
    const m = toMultiSeries({ columns: ['region', 'revenue'], rows: [['East', 1], ['West', 2]] })
    expect(m.shape).toBe('single')
    expect(m.series).toHaveLength(1)
    expect(m.labels).toEqual(['East', 'West'])
    // a column named value/total wins over a wide reading, as toPoints always did
    const named = toMultiSeries({ columns: ['k', 'a', 'value'], rows: [['x', 1, 2], ['y', 3, 4]] })
    expect(named.shape).toBe('single')
    expect(named.series[0].values).toEqual([2, 4])
  })

  it('does not merge repeated labels of a single series (they stay separate points)', () => {
    const m = toMultiSeries({ columns: ['k', 'v'], rows: [['a', 1], ['a', 2]] })
    expect(m.labels).toEqual(['a', 'a'])
    expect(m.series[0].values).toEqual([1, 2])
  })

  it('reads a numeric first column as the label of a long result', () => {
    const m = toMultiSeries({ columns: ['year', 'team', 'wins'], rows: [[2024, 'A', 5], [2024, 'B', 6], [2025, 'A', 7]] })
    expect(m.shape).toBe('long')
    expect(m.labels).toEqual(['2024', '2025'])
    expect(names(m)).toEqual(['A', 'B'])
  })

  it('a column named series makes any width long, wherever the columns sit', () => {
    const named = toMultiSeries({
      columns: ['series', 'label', 'extra', 'value'],
      rows: [['Ana', 'Jan', 'x', 3], ['Ben', 'Jan', 'y', 4]],
    })
    expect(named.shape).toBe('long')
    expect(named.labels).toEqual(['Jan'])
    expect(named.series).toEqual([{ name: 'Ana', values: [3] }, { name: 'Ben', values: [4] }])
  })

  it('uses declared types over sampled values', () => {
    // "2025" strings are numeric by sampling, but the column is VARCHAR
    const rows = [['2025', 'Ana', 1], ['2025', 'Ben', 2]]
    expect(toMultiSeries({ columns: ['y', 's', 'v'], rows, types: ['VARCHAR', 'VARCHAR', 'BIGINT'] }).shape).toBe('long')
  })

  it('is ambiguous-safe: three numeric columns are wide, never long', () => {
    expect(toMultiSeries({ columns: ['a', 'b', 'c'], rows: [[1, 2, 3], [4, 5, 6]] }).shape).toBe('wide')
  })

  it('returns nothing for an empty result', () => {
    expect(toMultiSeries({ columns: ['a', 'b'], rows: [] }).series).toEqual([])
    expect(toMultiSeries({ columns: [], rows: [] }).labels).toEqual([])
  })
})

describe('toMultiSeries: values', () => {
  it('leaves a missing point as a gap, not a zero', () => {
    const m = toMultiSeries({
      columns: ['m', 's', 'v'],
      rows: [['Jan', 'A', 1], ['Feb', 'A', 2], ['Jan', 'B', 5]],
    })
    expect(m.series[1].values).toEqual([5, null])
  })

  it('reads nulls, blanks and text values as gaps, and numeric strings as numbers', () => {
    const m = toMultiSeries({
      columns: ['m', 's', 'v'],
      rows: [['Jan', 'A', '12.5'], ['Feb', 'A', null], ['Mar', 'A', ''], ['Apr', 'A', 'n/a'], ['May', 'A', true]],
    })
    expect(m.series[0].values).toEqual([12.5, null, null, null, null])
  })

  it('sums a repeated (label, series) pair', () => {
    const m = toMultiSeries({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', 1], ['Jan', 'A', 2], ['Jan', 'B', 4]] })
    expect(m.series[0].values).toEqual([3])
  })

  it('names a null series (blank)', () => {
    const m = toMultiSeries({ columns: ['m', 's', 'v'], rows: [['Jan', null, 1], ['Jan', 'B', 2]] })
    expect(names(m)).toEqual(['(blank)', 'B'])
  })

  it('treats a huge numeric string as a number', () => {
    const m = toMultiSeries({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', '123456789012345678901'], ['Jan', 'B', 1]] })
    expect(m.series[0].values[0]).toBeGreaterThan(1e20)
  })
})

describe('series fold', () => {
  const nine = (extra?: (i: number) => string) => {
    const rows: any[][] = []
    for (let s = 1; s <= 9; s++) for (const m of ['Jan', 'Feb']) rows.push([m, extra ? extra(s) : `S${s}`, s])
    return toMultiSeries({ columns: ['m', 's', 'v'], rows })
  }

  it('keeps the five largest in their own order and sums the rest into Other', () => {
    const m = nine()
    expect(names(m)).toEqual(['S5', 'S6', 'S7', 'S8', 'S9', 'Other'])
    expect(m.folded).toEqual(['S1', 'S2', 'S3', 'S4'])
    expect(m.series[5].values).toEqual([10, 10])
  })

  it('does not fold six', () => {
    const m = toMultiSeries({ columns: ['m', 'a', 'b', 'c', 'd', 'e', 'f'], rows: [['x', 1, 2, 3, 4, 5, 6]] })
    expect(m.series).toHaveLength(6)
    expect(m.folded).toEqual([])
  })

  it('leaves a gap in Other only where every folded series has none', () => {
    const series = Array.from({ length: 7 }, (_, i) => ({ name: `S${i}`, values: [i === 0 ? 1 : null, 100 + i, null] as (number | null)[] }))
    const f = foldSeries(series)
    const other = f.series.find((s) => s.name === 'Other')!
    // S0 and S1 are folded: label 1 sums 100 + 101, label 2 is empty for both
    expect(other.values).toEqual([1, 201, null])
  })

  it('merges a real series named Other into the fold instead of colliding with it', () => {
    const rows: any[][] = []
    for (let s = 1; s <= 6; s++) rows.push(['Jan', `S${s}`, s])
    rows.push(['Jan', 'Other', 1000])
    const m = toMultiSeries({ columns: ['m', 's', 'v'], rows })
    expect(names(m).filter((n) => n === 'Other')).toHaveLength(1)
    expect(names(m)).toEqual(['S2', 'S3', 'S4', 'S5', 'S6', 'Other'])
    expect(m.series[5].values).toEqual([1001])
    expect(m.folded).toContain('Other')
  })

  it('leaves a lone Other alone when nothing needs folding', () => {
    const m = toMultiSeries({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', 1], ['Jan', 'Other', 2]] })
    expect(names(m)).toEqual(['A', 'Other'])
    expect(m.folded).toEqual([])
  })

  it('ranks by magnitude, so a large negative series is kept', () => {
    const rows: any[][] = []
    for (let s = 1; s <= 7; s++) rows.push(['Jan', `S${s}`, s === 1 ? -50 : s])
    expect(names(toMultiSeries({ columns: ['m', 's', 'v'], rows }))).toContain('S1')
  })
})

describe('inline series', () => {
  it('unions labels in first-seen order and leaves gaps', () => {
    const m = fromInlineSeries([
      { name: 'A', points: [{ label: 'Jan', value: 1 }, { label: 'Feb', value: 2 }] },
      { name: 'B', points: [{ label: 'Feb', value: 5 }, { label: 'Mar', value: 6 }] },
    ])
    expect(m.shape).toBe('inline')
    expect(m.labels).toEqual(['Jan', 'Feb', 'Mar'])
    expect(m.series).toEqual([{ name: 'A', values: [1, 2, null] }, { name: 'B', values: [null, 5, 6] }])
  })

  it('folds past six like a query result does', () => {
    const list = Array.from({ length: 8 }, (_, i) => ({ name: `S${i}`, points: [{ label: 'x', value: i + 1 }] }))
    expect(fromInlineSeries(list).series).toHaveLength(6)
  })

  it('tolerates a series with no points', () => {
    expect(fromInlineSeries([{ name: 'A', points: [] as any }]).series).toEqual([{ name: 'A', values: [] }])
  })
})

describe('fromPoints / isSeriesShape', () => {
  it('shapes single points', () => {
    expect(fromPoints([{ label: 'a', value: 1 }]).series[0].values).toEqual([1])
  })
  it('says when a result has to go to the chart whole', () => {
    expect(isSeriesShape({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', 1], ['Jan', 'B', 2]] })).toBe(true)
    // one series left by a filter is still read by its series and value columns
    expect(isSeriesShape({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', 1], ['Feb', 'A', 2]] })).toBe(true)
    expect(isSeriesShape({ columns: ['m', 'a', 'b'], rows: [['Jan', 1, 2]] })).toBe(true)
    expect(isSeriesShape({ columns: ['m', 'v'], rows: [['Jan', 1]] })).toBe(false)
  })
})
