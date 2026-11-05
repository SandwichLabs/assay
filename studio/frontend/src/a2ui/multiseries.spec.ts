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
    const rows = [['2025', 'Ana', 1], ['2025', 'Ben', 2], ['2026', 'Ana', 3], ['2026', 'Ben', 4]]
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
      columns: ['m', 'series', 'v'],
      rows: [['Jan', 'A', 1], ['Feb', 'A', 2], ['Jan', 'B', 5]],
    })
    expect(m.series[1].values).toEqual([5, null])
  })

  it('reads nulls, blanks and text values as gaps, and numeric strings as numbers', () => {
    const m = toMultiSeries({
      columns: ['m', 'series', 'v'],
      rows: [['Jan', 'A', '12.5'], ['Feb', 'A', null], ['Mar', 'A', ''], ['Apr', 'A', 'n/a'], ['May', 'A', true], ['Jan', 'B', 1]],
    })
    expect(m.series[0].values).toEqual([12.5, null, null, null, null])
  })

  it('sums a repeated (label, series) pair', () => {
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['Jan', 'A', 1], ['Jan', 'A', 2], ['Jan', 'B', 4]] })
    expect(m.series[0].values).toEqual([3])
  })

  it('names a null series (blank)', () => {
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['Jan', null, 1], ['Jan', 'B', 2]] })
    expect(names(m)).toEqual(['(blank)', 'B'])
  })

  it('treats a huge numeric string as a number', () => {
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['Jan', 'A', '123456789012345678901'], ['Jan', 'B', 1]] })
    expect(m.series[0].values[0]).toBeGreaterThan(1e20)
  })
})

describe('series fold', () => {
  const nine = (extra?: (i: number) => string) => {
    const rows: any[][] = []
    for (let s = 1; s <= 9; s++) for (const m of ['Jan', 'Feb']) rows.push([m, extra ? extra(s) : `S${s}`, s])
    return toMultiSeries({ columns: ['m', 'series', 'v'], rows })
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
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows })
    expect(names(m).filter((n) => n === 'Other')).toHaveLength(1)
    expect(names(m)).toEqual(['S2', 'S3', 'S4', 'S5', 'S6', 'Other'])
    expect(m.series[5].values).toEqual([1001])
    expect(m.folded).toContain('Other')
  })

  it('leaves a lone Other alone when nothing needs folding', () => {
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['Jan', 'A', 1], ['Jan', 'Other', 2]] })
    expect(names(m)).toEqual(['A', 'Other'])
    expect(m.folded).toEqual([])
  })

  it('ranks by magnitude, so a large negative series is kept', () => {
    const rows: any[][] = []
    for (let s = 1; s <= 7; s++) rows.push(['Jan', `S${s}`, s === 1 ? -50 : s])
    expect(names(toMultiSeries({ columns: ['m', 'series', 'v'], rows }))).toContain('S1')
  })
})

describe('series fold for lines (drop)', () => {
  const nine = () => {
    const rows: any[][] = []
    for (let s = 1; s <= 9; s++) for (const m of ['Jan', 'Feb']) rows.push([m, `S${s}`, s])
    return toMultiSeries({ columns: ['m', 'series', 'v'], rows }, 'drop')
  }

  it('keeps the six largest in their own order, invents no Other, and names the rest', () => {
    const m = nine()
    expect(names(m)).toEqual(['S4', 'S5', 'S6', 'S7', 'S8', 'S9'])
    expect(m.folded).toEqual(['S1', 'S2', 'S3'])
    expect(m.foldMode).toBe('drop')
  })
  it('does not fold six, and names nothing', () => {
    const m = toMultiSeries({ columns: ['m', 'a', 'b', 'c', 'd', 'e', 'f'], rows: [['x', 1, 2, 3, 4, 5, 6]] }, 'drop')
    expect(m.series).toHaveLength(6)
    expect(m.folded).toEqual([])
  })
  it('ranks a real series named Other like any other', () => {
    const rows: any[][] = []
    for (let s = 1; s <= 6; s++) rows.push(['Jan', `S${s}`, s])
    rows.push(['Jan', 'Other', 1000])
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows }, 'drop')
    expect(names(m)).toEqual(['S2', 'S3', 'S4', 'S5', 'S6', 'Other'])
    expect(m.folded).toEqual(['S1'])
    const small = toMultiSeries({ columns: ['m', 'series', 'v'], rows: [...rows.slice(0, 6), ['Jan', 'Other', 0.5]] }, 'drop')
    expect(names(small)).not.toContain('Other')
    expect(small.folded).toEqual(['Other'])
  })
  it('breaks ties in original order and ranks by magnitude', () => {
    const rows: any[][] = [['a', 'S0', -50]]
    for (let s = 1; s <= 7; s++) rows.push(['a', `S${s}`, 5])
    const m = toMultiSeries({ columns: ['m', 'series', 'v'], rows }, 'drop')
    expect(names(m)).toEqual(['S0', 'S1', 'S2', 'S3', 'S4', 'S5'])
    expect(m.folded).toEqual(['S6', 'S7'])
  })
  it('folds inline series the same way', () => {
    const list = Array.from({ length: 8 }, (_, i) => ({ name: `S${i}`, points: [{ label: 'x', value: i + 1 }] }))
    const m = fromInlineSeries(list, 'drop')
    expect(m.series).toHaveLength(6)
    expect(m.folded).toEqual(['S0', 'S1'])
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
    expect(isSeriesShape({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', 1], ['Jan', 'B', 2], ['Feb', 'A', 3]] })).toBe(true)
    // a one-to-one label/text pairing is an attribute, not a series
    expect(isSeriesShape({ columns: ['m', 's', 'v'], rows: [['Jan', 'A', 1], ['Feb', 'A', 2]] })).toBe(false)
    // ...unless the column says it is the series
    expect(isSeriesShape({ columns: ['m', 'series', 'v'], rows: [['Jan', 'A', 1], ['Feb', 'A', 2]] })).toBe(true)
    expect(isSeriesShape({ columns: ['m', 'a', 'b'], rows: [['Jan', 1, 2]] })).toBe(true)
    expect(isSeriesShape({ columns: ['m', 'v'], rows: [['Jan', 1]] })).toBe(false)
  })
})

describe('three columns that are not (label, series, value)', () => {
  it('(id, name, total) is one series labelled by name', () => {
    const m = toMultiSeries({ columns: ['customer_id', 'name', 'total'], rows: [[1, 'Ada', 5], [2, 'Bo', 7], [3, 'Cy', 2]] })
    expect(m.shape).toBe('single')
    expect(m.labels).toEqual(['Ada', 'Bo', 'Cy'])
    expect(m.series[0]).toEqual({ name: 'total', values: [5, 7, 2] })
  })
  it('(day, top_product, sales) is one series by day, charted by the numeric column', () => {
    const m = toMultiSeries({ columns: ['day', 'top_product', 'sales'], rows: [['2025-01-01', 'A', 5], ['2025-01-02', 'B', 7]] })
    expect(m.shape).toBe('single')
    expect(m.labels).toEqual(['2025-01-01', '2025-01-02'])
    expect(m.series[0].values).toEqual([5, 7])
  })
  it('a label that carries two series is long, even when other labels carry one', () => {
    const m = toMultiSeries({ columns: ['day', 'channel', 'units'], rows: [['a', 'web', 1], ['a', 'store', 2], ['b', 'web', 3]] })
    expect(m.shape).toBe('long')
  })
  it('a second column named label or name keeps the result single; a first one does not', () => {
    expect(toMultiSeries({ columns: ['m', 'name', 'v'], rows: [['a', 'x', 1], ['a', 'y', 2], ['b', 'x', 3]] }).shape).toBe('single')
    expect(toMultiSeries({ columns: ['label', 'rep', 'value'], rows: [['a', 'x', 1], ['a', 'y', 2], ['b', 'x', 3]] }).shape).toBe('long')
  })
  it('one tie in a one-to-one text column is still an attribute', () => {
    const rows = [['d1', 'P1', 5], ['d1', 'P2', 5], ['d2', 'P3', 7], ['d3', 'P4', 8]]
    expect(toMultiSeries({ columns: ['day', 'top_product', 'sales'], rows }).shape).toBe('single')
  })
})

describe('label order and merging', () => {
  it('keeps January first when the first series lacks it (ORDER BY rep, month)', () => {
    const rows: any[][] = [['Feb', 'Ana', 2], ['Mar', 'Ana', 3], ['Jan', 'Ben', 1], ['Feb', 'Ben', 2], ['Mar', 'Ben', 3]]
    expect(toMultiSeries({ columns: ['m', 'series', 'v'], rows }).labels).toEqual(['Jan', 'Feb', 'Mar'])
  })
  it('sorts all-numeric labels by value', () => {
    const rows: any[][] = [[10, 'A', 1], [9, 'A', 2], [10, 'B', 3], [9, 'B', 4]]
    expect(toMultiSeries({ columns: ['n', 's', 'v'], rows }).labels).toEqual(['9', '10'])
  })
  it('says when pairs were summed', () => {
    expect(toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['a', 'A', 1], ['a', 'A', 2], ['a', 'B', 1]] }).merged).toBe(true)
    expect(toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['a', 'A', 1], ['a', 'B', 2]] }).merged).toBe(false)
    expect(toMultiSeries({ columns: ['m', 'series', 'v'], rows: [['a', 'A', 1], ['a', 'A', 2]] }).merged).toBe(true)
  })
})

describe('label order: a stable topological merge', () => {
  const order = (rows: any[][]) => toMultiSeries({ columns: ['m', 'series', 'v'], rows }).labels
  it('a label first seen by a later series stays where the series put it', () => {
    expect(order([['Jan', 'Ana', 1], ['Jan', 'Ben', 1], ['Feb', 'Cy', 1], ['Mar', 'Cy', 1], ['Mar', 'Ana', 1]])).toEqual(['Jan', 'Feb', 'Mar'])
    expect(order([['Jan', 'Ana', 1], ['Jan', 'Ben', 1], ['Feb', 'Ben', 1], ['Mar', 'Ana', 1], ['Apr', 'Cy', 1], ['May', 'Ana', 1]])).toEqual(['Jan', 'Feb', 'Mar', 'Apr', 'May'])
  })
  it('keeps first-seen order for a tie', () => {
    const rows = [['d1', 'P1', 1], ['d1', 'P2', 1], ['d2', 'P3', 1], ['d3', 'P1', 1], ['d4', 'P4', 1], ['d5', 'P2', 1]]
    expect(order(rows)).toEqual(['d1', 'd2', 'd3', 'd4', 'd5'])
  })
  it('falls back to first-seen order on a cycle', () => {
    // Ana: a before b; Ben: b before a
    expect(order([['a', 'Ana', 1], ['b', 'Ana', 1], ['b', 'Ben', 1], ['a', 'Ben', 1]])).toEqual(['a', 'b'])
  })
})
