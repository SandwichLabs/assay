// annotations.ts — reading the markers, where they land, and the label layout.
import { describe, it, expect } from 'vitest'
import {
  MAX_ANNOTATIONS, LANE_PX, MAX_LANES, ariaText, fromResult, fromRows, layoutMarkers, noteText, placeOnAxis, placeOnColumns,
  type Annotation, type PlacedMarker,
} from './annotations'
import { buildXAxis } from './xaxis'

const ann = (at: string, label = 'event', extra: Partial<Annotation> = {}): Annotation => ({ at, label, kind: 'solid', color: 'muted', ...extra })
const weeks = ['2025-04-07', '2025-04-14', '2025-04-21', '2025-04-28', '2025-05-05']

describe('fromRows', () => {
  it('reads at, label, kind and color, defaulting to solid and muted', () => {
    const p = fromRows([{ at: '2025-05-01', label: 'Price cut' }, { at: '2025-05-02', label: 'Sale', kind: 'dashed', color: 'error' }])
    expect(p.items).toEqual([ann('2025-05-01', 'Price cut'), ann('2025-05-02', 'Sale', { kind: 'dashed', color: 'error' })])
    expect(p).toMatchObject({ over: 0, skipped: 0, unknown: 0 })
  })
  it('skips rows without a usable at or label, and counts them', () => {
    const p = fromRows([{ at: '2025-05-01' }, { label: 'x' }, { at: '  ', label: 'x' }, 'nope', { at: 2024, label: 'year' }])
    expect(p.items).toEqual([ann('2024', 'year')])
    expect(p.skipped).toBe(4)
  })
  it('caps at 50 and counts what it dropped', () => {
    const rows = Array.from({ length: 53 }, (_, i) => ({ at: `2025-01-${String((i % 28) + 1).padStart(2, '0')}`, label: `e${i}` }))
    const p = fromRows(rows)
    expect(p.items).toHaveLength(MAX_ANNOTATIONS)
    expect(p.over).toBe(3)
    expect(p.items[49].label).toBe('e49')
  })
  it('ignores a non-array', () => {
    expect(fromRows(undefined).items).toEqual([])
    expect(fromRows('x' as any).items).toEqual([])
  })
})

describe('fromResult', () => {
  it('reads columns by name, in any order', () => {
    const p = fromResult({ columns: ['event_name', 'kind', 'label', 'at'], rows: [['n', 'dashed', 'Launch', '2025-05-01']] })
    expect(p.items).toEqual([ann('2025-05-01', 'Launch', { kind: 'dashed' })])
  })
  it('falls back to the first and second column', () => {
    const p = fromResult({ columns: ['start_date', 'event_name'], rows: [['2025-05-01', 'Launch'], [null, 'No date']] })
    expect(p.items).toEqual([ann('2025-05-01', 'Launch')])
    expect(p.skipped).toBe(1)
  })
  it('uses the first column for label when at is not first', () => {
    const p = fromResult({ columns: ['name', 'at'], rows: [['Launch', '2025-05-01']] })
    expect(p.items).toEqual([ann('2025-05-01', 'Launch')])
  })
  it('counts a kind or color outside the closed sets and draws it solid and muted', () => {
    const p = fromResult({ columns: ['at', 'label', 'kind', 'color'], rows: [['2025-05-01', 'x', 'permanent', 'purple']] })
    expect(p.items).toEqual([ann('2025-05-01', 'x')])
    expect(p.unknown).toBe(1)
  })
  it('drops rows past 50, with the count', () => {
    const rows = Array.from({ length: 60 }, (_, i) => [`2025-01-01`, `e${i}`])
    const p = fromResult({ columns: ['at', 'label'], rows })
    expect(p.items).toHaveLength(50)
    expect(p.over).toBe(10)
  })
  it('accepts a number or a Date as at', () => {
    const p = fromResult({ columns: ['at', 'label'], rows: [[2024, 'y'], [new Date('2025-05-01T00:00:00Z'), 'd']] })
    expect(p.items.map((i) => i.at)).toEqual(['2024', '2025-05-01T00:00:00.000Z'])
  })
})

describe('placeOnAxis: time axis', () => {
  const xa = buildXAxis(weeks, 600, 640)
  it('puts a date at its time, between the weekly points, in the bucket before it', () => {
    const { markers, outside } = placeOnAxis([ann('2025-04-17', 'mid-week'), ann('2025-04-14', 'on a point')], weeks, xa)
    expect(outside).toBe(0)
    expect(markers[0].frac).toBeCloseTo(10 / 28)
    expect(markers[0].bucket).toBe(1)
    expect(markers[1].frac).toBeCloseTo(7 / 28)
    expect(markers[1].bucket).toBe(1)
  })
  it('keeps the ends and drops what is outside the first and last label', () => {
    const { markers, outside } = placeOnAxis([ann('2025-04-07'), ann('2025-05-05'), ann('2025-04-06'), ann('2025-05-06'), ann('2024-01-01')], weeks, xa)
    expect(markers.map((m) => m.frac)).toEqual([0, 1])
    expect(outside).toBe(3)
  })
  it('accepts a timestamp and a date with a zone', () => {
    const { markers } = placeOnAxis([ann('2025-04-21 12:00:00'), ann('2025-04-21T00:00:00Z')], weeks, xa)
    expect(markers[0].frac).toBeGreaterThan(markers[1].frac)
  })
  it('drops an at that is not a date', () => {
    const { markers, outside } = placeOnAxis([ann('Q2'), ann('2025-04-21')], weeks, xa)
    expect(markers).toHaveLength(1)
    expect(outside).toBe(1)
  })
  it('works for unsorted labels (the bucket is by time, not by position)', () => {
    const shuffled = ['2025-04-28', '2025-04-07', '2025-04-21', '2025-04-14']
    const { markers } = placeOnAxis([ann('2025-04-16')], shuffled, buildXAxis(shuffled, 600, 640))
    expect(markers[0].bucket).toBe(3)
  })
})

describe('placeOnAxis: category axis', () => {
  const labels = ['north', 'south', 'east']
  const xa = buildXAxis(labels, 600, 640)
  it('puts a marker on the label it names, at that label x', () => {
    const { markers, outside } = placeOnAxis([ann('south'), ann('east'), ann('west')], labels, xa)
    expect(markers.map((m) => [m.frac, m.bucket])).toEqual([[0.5, 1], [1, 2]])
    expect(outside).toBe(1)
  })
  it('matches a numeric category by its text', () => {
    const years = ['2023', '2024', '2025']
    const { markers } = placeOnAxis([ann('2024')], years, buildXAxis(years, 600, 640))
    // bare years are categories (not dates), so it matches the label text
    expect(markers).toHaveLength(1)
    expect(markers[0].bucket).toBe(1)
  })
})

describe('placeOnColumns', () => {
  it('centres a marker on its named column', () => {
    const { markers, outside } = placeOnColumns([ann('b'), ann('z')], ['a', 'b', 'c', 'd'], true)
    expect(markers.map((m) => [m.frac, m.bucket])).toEqual([[0.375, 1]])
    expect(outside).toBe(1)
  })
  it('puts a date on the column it falls in, up to one column past the last', () => {
    const labels = ['2025-01', '2025-02', '2025-03']
    const { markers, outside } = placeOnColumns([ann('2025-02-15'), ann('2025-03-20'), ann('2025-04-30'), ann('2024-12-31')], labels, true)
    expect(markers.map((m) => m.bucket)).toEqual([1, 2])
    expect(markers[0].frac).toBeCloseTo(1.5 / 3)
    expect(outside).toBe(2)
  })
  it('does not bucket dates for columns sorted by size', () => {
    const labels = ['2025-03', '2025-01', '2025-02']
    const { markers, outside } = placeOnColumns([ann('2025-02-15'), ann('2025-01')], labels, false)
    expect(markers.map((m) => m.bucket)).toEqual([1])
    expect(outside).toBe(1)
  })
  it('does not bucket dates for columns out of time order', () => {
    const { outside } = placeOnColumns([ann('2025-02-15')], ['2025-03', '2025-02', '2025-01'], true)
    expect(outside).toBe(1)
  })
})

describe('layoutMarkers', () => {
  const at = (frac: number, label: string, key = 0): PlacedMarker => ({ key, ann: ann('x', label), frac, bucket: 0 })
  it('puts well separated labels all on the row nearest the plot', () => {
    const l = layoutMarkers([at(0.1, 'one', 0), at(0.5, 'two', 1), at(0.9, 'xx', 2)], 600)
    expect(l.markers.map((m) => m.lane)).toEqual([0, 0, 0])
    expect(l.lanes).toBe(1)
    expect(l.hidden).toBe(0)
  })
  it('staggers labels that would collide, higher rows to the left', () => {
    const l = layoutMarkers([at(0.5, 'Starter pack price cut', 0), at(0.52, 'Double XP weekend', 1), at(0.54, 'Season pass', 2)], 600)
    const lane = Object.fromEntries(l.markers.map((m) => [m.key, m.lane]))
    expect(lane[2]).toBe(0)
    expect(lane[1]).toBe(1)
    expect(lane[0]).toBe(2)
    expect(l.lanes).toBe(3)
  })
  it('never lets a rule pass through another marker label', () => {
    const l = layoutMarkers([at(0.3, 'a very long event label', 0), at(0.35, 'b', 1), at(0.4, 'c', 2), at(0.45, 'dd', 3)], 600)
    for (const m of l.markers) {
      for (const o of l.markers) {
        if (m === o || m.lane === null || o.lane === null) continue
        const x = m.frac * 600
        const ox = o.frac * 600
        const a = ox + o.shift
        const inside = x > a + 2 && x < a + o.width - 2
        // a rule reaches every row below its own label's row
        if (inside) expect(m.lane, `${m.key} vs ${o.key}`).toBeLessThan(o.lane)
      }
    }
  })
  it('keeps a label inside the plot near the right edge by shifting it left', () => {
    const l = layoutMarkers([at(0.98, 'Season finale event')], 600)
    expect(l.markers[0].shift).toBeLessThan(0)
    expect(l.markers[0].frac * 600 + l.markers[0].shift + l.markers[0].width).toBeLessThanOrEqual(600.0001)
  })
  it('lays two markers at the same position on separate rows', () => {
    const l = layoutMarkers([at(0.5, 'first', 0), at(0.5, 'second', 1)], 600)
    expect(new Set(l.markers.map((m) => m.lane)).size).toBe(2)
    expect(l.hidden).toBe(0)
  })
  it('hides labels, keeping the rules, when more rows would be needed than fit', () => {
    const many = Array.from({ length: 8 }, (_, i) => at(0.5 + i * 0.002, `event number ${i}`, i))
    const l = layoutMarkers(many, 600)
    expect(l.lanes).toBeLessThanOrEqual(MAX_LANES)
    expect(l.hidden).toBe(8 - MAX_LANES)
    expect(l.markers).toHaveLength(8)
    expect(l.markers.filter((m) => m.lane === null)).toHaveLength(l.hidden)
  })
  it('truncates long labels and shortens the cap on a narrow chart', () => {
    const long = 'x'.repeat(60)
    const wide = layoutMarkers([at(0.1, long)], 800).markers[0]
    const narrow = layoutMarkers([at(0.1, long)], 240).markers[0]
    expect(wide.text.endsWith('…')).toBe(true)
    expect([...narrow.text].length).toBeLessThan([...wide.text].length)
  })
  it('is empty for no markers and uses a default width before the plot is measured', () => {
    expect(layoutMarkers([], 0)).toEqual({ markers: [], lanes: 0, hidden: 0 })
    expect(layoutMarkers([at(0.5, 'x')], 0).lanes).toBe(1)
  })
  it('rows are LANE_PX tall', () => {
    expect(LANE_PX).toBeGreaterThanOrEqual(14)
  })
})

describe('text', () => {
  it('notes only what happened', () => {
    expect(noteText({ over: 0, skipped: 0, unknown: 0, outside: 0, hidden: 0 })).toBe('')
    const t = noteText({ over: 3, skipped: 1, unknown: 2, outside: 2, hidden: 1, error: 'boom' })
    expect(t).toContain('query error: boom')
    expect(t).toContain("2 markers are outside this chart's range and not shown")
    expect(t).toContain('3 markers past the limit of 50 not shown')
    expect(t).toContain('1 row without a usable at and label skipped')
    expect(t).toContain('2 markers have a kind or color other than')
    expect(t).toContain('1 label is hidden to avoid overlap')
  })
  it('lists markers left to right for the text alternative', () => {
    const m = (frac: number, a: Annotation): PlacedMarker => ({ key: 0, ann: a, frac, bucket: 0 })
    expect(ariaText([])).toBe('')
    expect(ariaText([m(0.8, ann('2025-05-01', 'Price cut')), m(0.1, ann('2025-01-01', 'Launch'))])).toBe(
      '. 2 event markers: Launch (2025-01-01); Price cut (2025-05-01)',
    )
    expect(ariaText([m(0.1, ann('north', 'north'))])).toBe('. 1 event marker: north')
  })
})
