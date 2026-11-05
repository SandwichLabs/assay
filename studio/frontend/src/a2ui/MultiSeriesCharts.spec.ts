// Multi-series lineChart and barChart (#13 item 2): the shape detection is in
// multiseries.spec.ts; this is what the renderers draw from it.
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import A2UIPage from './A2UIPage.vue'
import { ARTIFACT_RUNTIME } from './artifactRuntime'
import type { A2UIComponentT } from './types'
import { synthesizedPage, newCellState } from '../lib/notebookCells'

function render(c: A2UIComponentT) {
  return mount(A2UIPage, { props: { page: { title: 't', components: [c] } } })
}

const months = ['2025-01', '2025-02', '2025-03', '2025-04']
const longRows = (reps: string[]) => months.flatMap((m, i) => reps.map((r, j) => [m, r, (j + 1) * 100 + i * 10]))
const long = (reps: string[], extra: Record<string, any> = {}): A2UIComponentT => ({
  id: 'c', type: 'lineChart', columns: ['month', 'rep', 'revenue'], rows: longRows(reps), ...extra,
})

describe('multi-series lineChart', () => {
  it('draws one coloured line per series on a shared axis, with a legend', () => {
    const w = render(long(['Ana', 'Ben', 'Cy', 'Di']))
    expect(w.findAll('g.a2ui-linechart__series')).toHaveLength(4)
    expect(w.findAll('g.a2ui-linechart__series')[2].classes()).toContain('a2ui-series-2')
    expect(w.findAll('polyline.a2ui-linechart__line')).toHaveLength(4)
    const legend = w.find('[data-testid="a2ui-series-legend"]')
    expect(legend.text()).toContain('Ana')
    expect(legend.text()).toContain('Di')
    // one value axis: gridlines are drawn once
    expect(w.findAll('line.a2ui-linechart__grid').length).toBe(w.findAll('.a2ui-linechart__axis-y').length)
    // the axis reaches the biggest value of any series (430) from zero
    const y = w.findAll('.a2ui-linechart__axis-y').map((l) => Number(l.text().replace(/,/g, '')))
    expect(y[0]).toBe(0)
    expect(y[y.length - 1]).toBeGreaterThanOrEqual(430)
    expect(w.find('svg').attributes('aria-label')).toContain('4 series (Ana, Ben, Cy, Di)')
    w.unmount()
  })

  it('shares a time axis: the union of the series labels, sorted', () => {
    const w = render({
      id: 'c', type: 'lineChart',
      series: [
        { name: 'A', points: [{ label: '2025-03-01', value: 1 }, { label: '2025-01-01', value: 2 }] },
        { name: 'B', points: [{ label: '2025-02-01', value: 5 }] },
      ],
    })
    expect(w.findAll('.a2ui-linechart__axis-x').map((t) => t.text()).every((l) => /^[A-Z][a-z]{2} '25$/.test(l))).toBe(true)
    const cx = (g: number) => w.findAll('g.a2ui-linechart__series')[g].findAll('circle.a2ui-linechart__dot').map((c) => Number(c.attributes('cx')))
    // B's only point (Feb) sits between A's Jan and Mar points
    expect(cx(1)[0]).toBeGreaterThan(Math.min(...cx(0)))
    expect(cx(1)[0]).toBeLessThan(Math.max(...cx(0)))
    w.unmount()
  })

  it('leaves a gap, not a zero, where a series has no point', () => {
    const rows = [['2025-01', 'A', 10], ['2025-02', 'A', 20], ['2025-03', 'A', 30], ['2025-04', 'A', 40], ['2025-01', 'B', 5], ['2025-04', 'B', 8]]
    const w = render({ id: 'c', type: 'lineChart', columns: ['m', 's', 'v'], rows })
    const b = w.findAll('g.a2ui-linechart__series')[1]
    // B has two points with nothing between: two dots, and no line joining them through zero
    expect(b.findAll('circle.a2ui-linechart__dot')).toHaveLength(2)
    expect(b.findAll('polyline')).toHaveLength(0)
    expect(w.html()).not.toContain('NaN')
    w.unmount()
  })

  it('splits a line at a gap into runs', () => {
    const rows = [['a', 'A', 1], ['b', 'A', 2], ['c', 'A', null], ['d', 'A', 3], ['e', 'A', 4], ['a', 'B', 1]]
    const w = render({ id: 'c', type: 'lineChart', columns: ['m', 's', 'v'], rows })
    expect(w.findAll('g.a2ui-linechart__series')[0].findAll('polyline')).toHaveLength(2)
    w.unmount()
  })

  it('folds past six series into one Other and says so', () => {
    const reps = ['A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I']
    const w = render(long(reps))
    expect(w.findAll('g.a2ui-linechart__series')).toHaveLength(6)
    const legend = w.find('[data-testid="a2ui-series-legend"]')
    expect(legend.text()).toContain('Other')
    expect(w.find('[data-testid="a2ui-series-folded"]').text()).toBe('Other is the sum of 4 smaller series')
    // the biggest are kept: I (900s) and not A (100s)
    expect(legend.text()).toContain('I')
    expect(w.find('svg').attributes('aria-label')).toContain('6 series')
    w.unmount()
  })

  it('shows every series at the cursor label, by hover and by keyboard', async () => {
    const w = render(long(['Ana', 'Ben']))
    const plot = w.find('.a2ui-plot__body')
    await plot.trigger('focus')
    const tip = w.find('[data-testid="a2ui-linechart-tip"]')
    expect(tip.text()).toContain('2025-04')
    expect(tip.text()).toContain('Ana')
    expect(tip.text()).toContain('Ben')
    expect(tip.findAll('.a2ui-linechart__tip-value').map((v) => v.text())).toEqual(['130', '230'])
    expect(w.find('.a2ui-sr').text()).toBe('2025-04: Ana 130, Ben 230')
    await plot.trigger('keydown', { key: 'Home' })
    expect(w.find('[data-testid="a2ui-linechart-tip"]').text()).toContain('2025-01')
    expect(w.find('.a2ui-sr').text()).toBe('2025-01: Ana 100, Ben 200')
    expect(w.findAll('circle.a2ui-linechart__focus')).toHaveLength(2)
    w.unmount()
  })

  it('lists only the series that have a point at the cursor', async () => {
    const rows = [['a', 'A', 1], ['b', 'A', 2], ['b', 'B', 7]]
    const w = render({ id: 'c', type: 'lineChart', columns: ['m', 's', 'v'], rows })
    const plot = w.find('.a2ui-plot__body')
    await plot.trigger('focus')
    await plot.trigger('keydown', { key: 'Home' })
    expect(w.find('.a2ui-sr').text()).toBe('a: A 1')
    w.unmount()
  })

  it('reads a wide result, and keeps one series a plain single-colour line', () => {
    const wide = render({ id: 'c', type: 'lineChart', columns: ['m', 'cost', 'revenue'], rows: [['2025-01', 1, 5], ['2025-02', 2, 6]] })
    expect(wide.findAll('g.a2ui-linechart__series')).toHaveLength(2)
    wide.unmount()
    const one = render({ id: 'c', type: 'lineChart', color: 'error', points: [{ label: 'a', value: 1 }, { label: 'b', value: 2 }] })
    expect(one.findAll('g.a2ui-linechart__series')).toHaveLength(0)
    expect(one.find('svg').classes()).toContain('a2ui--error')
    expect(one.find('[data-testid="a2ui-series-legend"]').exists()).toBe(false)
    one.unmount()
  })

  it('does not take colour from the page when drawing several series', () => {
    const w = render(long(['Ana', 'Ben'], { color: 'error' }))
    expect(w.find('svg').classes()).not.toContain('a2ui--error')
    w.unmount()
  })

  it('shows NO ROWS for an empty result', () => {
    const w = render({ id: 'c', type: 'lineChart', columns: ['m', 's', 'v'], rows: [] })
    expect(w.text()).toContain('NO ROWS')
    w.unmount()
  })
})

describe('multi-series barChart (grouped)', () => {
  const bars = (reps: string[]): A2UIComponentT => ({
    id: 'b', type: 'barChart', columns: ['region', 'rep', 'revenue'],
    rows: ['East', 'West'].flatMap((r, i) => reps.map((s, j) => [r, s, (j + 1) * 100 + i * 50])),
  })

  it('draws a group per label with one coloured bar per series and a legend', () => {
    const w = render(bars(['Ana', 'Ben', 'Cy']))
    const groups = w.findAll('[data-testid="a2ui-bargroup"]')
    expect(groups).toHaveLength(2)
    const first = groups[0].findAll('.a2ui-bar--grouped')
    expect(first).toHaveLength(3)
    expect(first[1].classes()).toContain('a2ui-series-1')
    expect(groups[0].find('.a2ui-bar__label').text()).toBe('East')
    expect(groups[0].attributes('aria-label')).toBe('East: Ana 100, Ben 200, Cy 300')
    expect(groups[0].attributes('title')).toBe('East: Ana 100, Ben 200, Cy 300')
    expect(w.find('[data-testid="a2ui-series-legend"]').text()).toContain('Cy')
    w.unmount()
  })

  it('shares one scale: equal values are equal widths across series and groups', () => {
    const w = render({ id: 'b', type: 'barChart', columns: ['r', 's', 'v'], rows: [['E', 'A', 50], ['E', 'B', 100], ['W', 'A', 100], ['W', 'B', 25]] })
    const widths = w.findAll('.a2ui-bar__fill--series').map((f) => /width: ([0-9.]+)%/.exec(f.attributes('style')!)![1])
    expect(widths[1]).toBe(widths[2])
    expect(Number(widths[0])).toBeLessThan(Number(widths[1]))
    expect(Number(widths[3])).toBeLessThan(Number(widths[0]))
    w.unmount()
  })

  it('draws no bar for a missing value', () => {
    const w = render({ id: 'b', type: 'barChart', columns: ['r', 's', 'v'], rows: [['E', 'A', 5], ['E', 'B', 6], ['W', 'A', 7]] })
    const west = w.findAll('[data-testid="a2ui-bargroup"]')[1]
    expect(west.findAll('.a2ui-bar__fill--series')).toHaveLength(1)
    expect(west.attributes('aria-label')).toBe('W: A 7, B –')
    w.unmount()
  })

  it('folds past six series into Other', () => {
    const w = render(bars(['A', 'B', 'C', 'D', 'E', 'F', 'G']))
    expect(w.findAll('[data-testid="a2ui-bargroup"]')[0].findAll('.a2ui-bar--grouped')).toHaveLength(6)
    expect(w.find('[data-testid="a2ui-series-folded"]').exists()).toBe(true)
    w.unmount()
  })

  it('keeps a single series as the plain bars it always was', () => {
    const w = render({ id: 'b', type: 'barChart', bars: [{ label: 'a', value: 87 }, { label: 'b', value: 40 }] })
    expect(w.findAll('[data-testid="a2ui-bargroup"]')).toHaveLength(0)
    expect(w.findAll('.a2ui-bar')).toHaveLength(2)
    expect(w.find('[data-testid="a2ui-series-legend"]').exists()).toBe(false)
    w.unmount()
  })

  it('draws a negative grouped bar leftward from the shared zero line', () => {
    const w = render({ id: 'b', type: 'barChart', columns: ['r', 's', 'v'], rows: [['E', 'A', 50], ['E', 'B', -50]] })
    const [a, b] = w.findAll('.a2ui-bar__fill--series').map((f) => f.attributes('style')!)
    const num = (s: string, k: string) => Number(new RegExp(`${k}: ([0-9.]+)%`).exec(s)![1])
    expect(num(a, 'left')).toBeCloseTo(num(b, 'left') + num(b, 'width'), 0)
    w.unmount()
  })
})

describe('inside a data artifact view', () => {
  const result = {
    columns: ['month', 'rep', 'revenue'], types: ['VARCHAR', 'VARCHAR', 'BIGINT'],
    rows: [['2025-01', 'Ana', 1], ['2025-01', 'Ben', 2], ['2025-02', 'Ana', 3], ['2025-02', 'Ben', 4]],
  }
  const view = (c: A2UIComponentT) => {
    const rt = {
      result: () => ({ loading: false, error: '', data: result, fetched: true, truncated: false }),
      params: { param: () => undefined, display: () => '', error: () => '', isDefault: () => true, set: () => undefined, reset: () => undefined },
    }
    return mount(A2UIPage, { props: { page: { title: 't', components: [c] } }, global: { provide: { [ARTIFACT_RUNTIME as symbol]: rt } } })
  }

  it('a lineChart bound to a query draws the long shape as several series', () => {
    const w = view({ id: 'c', type: 'lineChart', query: 'q' })
    expect(w.findAll('g.a2ui-linechart__series')).toHaveLength(2)
    expect(w.find('[data-testid="a2ui-series-legend"]').text()).toContain('Ben')
    w.unmount()
  })

  it('a barChart bound to a query draws grouped bars, and its drill is off', () => {
    const w = view({ id: 'b', type: 'barChart', query: 'q', drill: { param: 'rep', column: 'rep' } })
    expect(w.findAll('[data-testid="a2ui-bargroup"]')).toHaveLength(2)
    expect(w.find('[role="button"]').exists()).toBe(false)
    w.unmount()
  })
})

describe('notebook cells hand a long or wide result to the chart whole', () => {
  const cell = (view: 'bar' | 'line', columns: string[], rows: any[][]) => {
    const c = newCellState({ id: 'x', type: 'sql', source: 'select 1' } as any)
    c.view = view
    c.result = { columns, rows, types: [], truncated: false } as any
    return synthesizedPage(c)!.components[0]
  }

  it('sends columns + rows for a (label, series, value) result', () => {
    const rows = [['Jan', 'Ana', 1], ['Jan', 'Ben', 2]]
    const c = cell('line', ['month', 'rep', 'revenue'], rows)
    expect(c.type).toBe('lineChart')
    expect(c.rows).toEqual(rows)
    expect(c.points).toBeUndefined()
    expect(cell('bar', ['month', 'rep', 'revenue'], rows).bars).toBeUndefined()
  })

  it('sends columns + rows for a wide result, and points for one series', () => {
    expect(cell('line', ['m', 'a', 'b'], [['Jan', 1, 2]]).rows).toBeDefined()
    expect(cell('line', ['m', 'v'], [['Jan', 1]]).points).toEqual([{ label: 'Jan', value: 1 }])
    expect(cell('bar', ['m', 'v'], [['Jan', 1]]).bars).toEqual([{ label: 'Jan', value: 1 }])
  })

  it('renders the synthesized long result as several series', () => {
    const rows = [['2025-01', 'Ana', 1], ['2025-01', 'Ben', 2], ['2025-02', 'Ana', 3], ['2025-02', 'Ben', 4]]
    const w = render({ ...cell('line', ['month', 'rep', 'revenue'], rows), id: 'c' })
    expect(w.findAll('g.a2ui-linechart__series')).toHaveLength(2)
    w.unmount()
  })
})
