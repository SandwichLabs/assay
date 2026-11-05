// Event markers (#320) on lineChart, areaChart and columnChart: what the
// renderers draw from `annotations`. Placement and layout are in annotations.spec.ts.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import A2UIPage from './A2UIPage.vue'
import { ARTIFACT_RUNTIME } from './artifactRuntime'
import { _resetQueryPoolForTests } from './query'
import type { A2UIComponentT } from './types'

function render(c: A2UIComponentT, runtime?: object) {
  return mount(A2UIPage, { props: { page: { title: 't', components: [c] } }, ...(runtime ? { global: { provide: { [ARTIFACT_RUNTIME as symbol]: runtime } } } : {}) })
}

function setApp(app: Record<string, any>) {
  ;(globalThis as any).window = (globalThis as any).window || {}
  ;(window as any).go = { main: { App: app } }
}

const weeks = ['2025-04-07', '2025-04-14', '2025-04-21', '2025-04-28', '2025-05-05']
const rows = weeks.map((w, i) => [w, 10 + i * 3])
const longRows = weeks.flatMap((w, i) => ['iOS', 'Android'].map((p, j) => [w, p, 10 + i + j * 5]))
const mk = (type: string, annotations: unknown, extra: Record<string, any> = {}): A2UIComponentT => ({
  id: 'c', type, columns: ['week', 'v'], rows, annotations, ...extra,
})
const markers = (w: ReturnType<typeof render>) => w.findAll('[data-testid="a2ui-annotation"]')

beforeEach(() => _resetQueryPoolForTests())

describe.each(['lineChart', 'areaChart', 'columnChart'])('%s annotations', (type) => {
  it('draws a rule and label per marker, in the colour and style given', () => {
    const w = render(mk(type, { rows: [
      { at: '2025-04-14', label: 'Starter pack price cut' },
      { at: '2025-04-28', label: 'Double XP', kind: 'dashed', color: 'error' },
    ] }))
    const m = markers(w)
    expect(m).toHaveLength(2)
    expect(m[0].classes()).toEqual(expect.arrayContaining(['a2ui--muted', 'a2ui-annot--solid']))
    expect(m[1].classes()).toEqual(expect.arrayContaining(['a2ui--error', 'a2ui-annot--dashed']))
    expect(m[0].text()).toContain('Starter pack price cut')
    expect(m[0].element.getAttribute('style')).toMatch(/left: [\d.]+%/)
    expect(w.find('[data-testid="a2ui-annotation-note"]').exists()).toBe(false)
    w.unmount()
  })

  it('lists the markers in the chart text alternative, and hides the overlay from assistive tech', () => {
    const w = render(mk(type, { rows: [{ at: '2025-04-14', label: 'Price cut' }] }))
    const aria = w.find('[role="img"]').attributes('aria-label')
    expect(aria).toContain('1 event marker: Price cut (2025-04-14)')
    expect(w.find('[data-testid="a2ui-annotations"]').attributes('aria-hidden')).toBe('true')
    w.unmount()
  })

  it('shows the full label and exact date in a tooltip on hover', async () => {
    const w = render(mk(type, { rows: [{ at: '2025-04-17', label: 'Price cut' }] }))
    expect(w.find('[data-testid="a2ui-annotation-tip"]').exists()).toBe(false)
    await markers(w)[0].trigger('mouseenter')
    const tip = w.find('[data-testid="a2ui-annotation-tip"]')
    expect(tip.text()).toContain('Price cut')
    expect(tip.text()).toContain('2025-04-17')
    await markers(w)[0].trigger('mouseleave')
    expect(w.find('[data-testid="a2ui-annotation-tip"]').exists()).toBe(false)
    w.unmount()
  })

  it('drops a marker outside the x range and says how many', () => {
    const w = render(mk(type, { rows: [{ at: '2025-04-14', label: 'in' }, { at: '2024-01-01', label: 'before' }, { at: '2026-01-01', label: 'after' }] }))
    expect(markers(w)).toHaveLength(1)
    expect(w.find('[data-testid="a2ui-annotation-note"]').text()).toContain("2 markers are outside this chart's range and not shown")
    w.unmount()
  })

  it('draws a chart without markers when annotations is empty', () => {
    const w = render(mk(type, { rows: [] }))
    expect(markers(w)).toHaveLength(0)
    expect(w.find('[data-testid="a2ui-annotation-note"]').exists()).toBe(false)
    w.unmount()
  })

  it('stacks the labels of close dates in separate rows above the plot', () => {
    const w = render(mk(type, { rows: [
      { at: '2025-04-14', label: 'Starter pack price cut' },
      { at: '2025-04-15', label: 'Double XP weekend' },
      { at: '2025-04-16', label: 'Season pass launch' },
    ] }))
    const tops = markers(w).map((m) => m.element.getAttribute('style')!.match(/top: (-?[\d.]+)px/)![1])
    expect(new Set(tops).size).toBe(3)
    // the chart makes room above the plot for the rows
    expect(w.html()).toMatch(/padding-top: 45px/)
    w.unmount()
  })

  it('uses no hex colour and no model string in a style', () => {
    const w = render(mk(type, { rows: [{ at: '2025-04-14', label: '<b>x</b>"; color: red', color: 'yellow' }] }))
    expect(w.find('[data-testid="a2ui-annotations"]').html()).not.toMatch(/#[0-9a-f]{3,8}\b/i)
    expect(w.find('.a2ui-annot__label').element.getAttribute('style')).not.toContain('red')
    expect(w.find('.a2ui-annot__label').find('b').exists()).toBe(false)
    w.unmount()
  })
})

describe('annotations from sql', () => {
  it('runs its own read-only binding on its connection, separately from the chart data', async () => {
    const run = vi.fn(async (sql: string, conn?: string) => {
      void sql; void conn
      return { columns: ['at', 'label', 'kind'], types: [], rows: [['2025-04-14', 'Launch', 'dashed'], ['2025-04-28', 'Patch', 'solid']] }
    })
    setApp({ RunA2UIQuery: vi.fn(), RunA2UIQueryIn: run })
    const w = render({ id: 'c', type: 'lineChart', columns: ['week', 'v'], rows, annotations: { sql: 'SELECT start_date AS at, name AS label, kind FROM live_ops_events', connection: 'gloamwick' } })
    await flushPromises()
    expect(run).toHaveBeenCalledTimes(1)
    expect(run.mock.calls[0][0]).toContain('live_ops_events')
    expect(run.mock.calls[0][1]).toBe('gloamwick')
    expect(markers(w)).toHaveLength(2)
    expect(markers(w)[0].classes()).toContain('a2ui-annot--dashed')
    w.unmount()
  })

  it('runs the chart sql and the annotation sql as two queries', async () => {
    const run = vi.fn(async (sql: string) =>
      sql.includes('events')
        ? { columns: ['at', 'label'], types: [], rows: [['2025-04-14', 'Launch']] }
        : { columns: ['week', 'v'], types: [], rows },
    )
    setApp({ RunA2UIQuery: run })
    const w = render({ id: 'c', type: 'areaChart', sql: 'SELECT week, v FROM t', annotations: { sql: 'SELECT d AS at, n AS label FROM events' } })
    await flushPromises()
    expect(run).toHaveBeenCalledTimes(2)
    expect(w.find('svg path.a2ui-area__line').exists()).toBe(true)
    expect(markers(w)).toHaveLength(1)
    w.unmount()
  })

  it('drops rows beyond 50 and says so', async () => {
    const many = Array.from({ length: 57 }, (_, i) => [weeks[i % weeks.length], `e${i}`])
    setApp({ RunA2UIQuery: vi.fn(async () => ({ columns: ['at', 'label'], types: [], rows: many })) })
    const w = render({ id: 'c', type: 'columnChart', columns: ['week', 'v'], rows, annotations: { sql: 'SELECT 1 AS at, 2 AS label' } })
    await flushPromises()
    expect(markers(w)).toHaveLength(50)
    expect(w.find('[data-testid="a2ui-annotation-note"]').text()).toContain('7 markers past the limit of 50 not shown')
    w.unmount()
  })

  it('keeps the chart and notes a failed annotation query', async () => {
    setApp({ RunA2UIQuery: vi.fn(async () => { throw new Error('read-only: nope') }) })
    const w = render({ id: 'c', type: 'lineChart', columns: ['week', 'v'], rows, annotations: { sql: 'DELETE FROM x' } })
    await flushPromises()
    expect(w.findAll('polyline.a2ui-linechart__line')).toHaveLength(1)
    expect(markers(w)).toHaveLength(0)
    expect(w.find('[data-testid="a2ui-annotation-note"]').text()).toContain('query error')
    w.unmount()
  })

  it('counts a kind outside solid/dashed in the note', async () => {
    setApp({ RunA2UIQuery: vi.fn(async () => ({ columns: ['at', 'label', 'kind'], types: [], rows: [['2025-04-14', 'x', 'permanent']] })) })
    const w = render({ id: 'c', type: 'lineChart', columns: ['week', 'v'], rows, annotations: { sql: 'SELECT 1, 2, 3' } })
    await flushPromises()
    expect(markers(w)[0].classes()).toContain('a2ui-annot--solid')
    expect(w.find('[data-testid="a2ui-annotation-note"]').text()).toContain('other than solid, dashed')
    w.unmount()
  })
})

describe('placement on the chart', () => {
  it('a category axis matches the label text, a column chart sits on the column centre', () => {
    const cols = { columns: ['region', 'v'], rows: [['north', 3], ['south', 5], ['east', 4], ['west', 6]] }
    const line = render({ id: 'c', type: 'lineChart', ...cols, annotations: { rows: [{ at: 'south', label: 'Opened' }, { at: 'moon', label: 'Nowhere' }] } })
    expect(markers(line)).toHaveLength(1)
    expect(markers(line)[0].element.getAttribute('style')).toContain('left: 33.333')
    expect(line.find('[data-testid="a2ui-annotation-note"]').text()).toContain('1 marker is outside')
    const col = render({ id: 'c', type: 'columnChart', ...cols, annotations: { rows: [{ at: 'south', label: 'Opened' }] } })
    expect(markers(col)[0].element.getAttribute('style')).toContain('left: 37.5%')
    line.unmount()
    col.unmount()
  })

  it('a date between weekly columns lands on the week it falls in', () => {
    const w = render(mk('columnChart', { rows: [{ at: '2025-04-17', label: 'Mid-week' }] }))
    expect(markers(w)[0].element.getAttribute('style')).toContain('left: 30%')
    w.unmount()
  })

  it('a time axis puts a mid-week date between the two weekly points', () => {
    const w = render(mk('lineChart', { rows: [{ at: '2025-04-17', label: 'Mid-week' }] }))
    // 10 of 28 days across
    expect(markers(w)[0].element.getAttribute('style')).toContain('left: 35.714')
    w.unmount()
  })

  it('puts the marker label into the line chart tooltip at its point', async () => {
    const w = render(mk('lineChart', { rows: [{ at: '2025-04-17', label: 'Mid-week' }] }))
    await w.find('[data-testid="a2ui-linechart-plot"]').trigger('keydown', { key: 'Home' })
    await w.find('[data-testid="a2ui-linechart-plot"]').trigger('keydown', { key: 'ArrowRight' })
    expect(w.find('[data-testid="a2ui-tip-annotation"]').text()).toContain('Mid-week (2025-04-17)')
    w.unmount()
  })
})

describe('multi-series line with annotations', () => {
  it('draws one line per series and the shared markers', () => {
    const w = render({
      id: 'c', type: 'lineChart', columns: ['week', 'platform', 'd7'], rows: longRows,
      annotations: { rows: [{ at: '2025-04-14', label: 'Release', color: 'yellow' }, { at: '2025-04-28', label: 'Price change', kind: 'dashed' }] },
    })
    expect(w.findAll('g.a2ui-linechart__series')).toHaveLength(2)
    expect(markers(w)).toHaveLength(2)
    expect(w.find('[data-testid="a2ui-series-legend"]').text()).toContain('Android')
    expect(w.find('svg').attributes('aria-label')).toContain('2 series')
    expect(w.find('svg').attributes('aria-label')).toContain('2 event markers')
    w.unmount()
  })
})

describe('inside a data artifact view', () => {
  const events = { columns: ['at', 'label'], types: [], rows: [['2025-04-14', 'Launch']] }
  const runtime = (seen: string[] = []) => ({
    result: (q: string) => {
      seen.push(q)
      return { loading: false, error: '', fetched: true, truncated: false, data: q === 'events' ? events : { columns: ['week', 'v'], types: [], rows } }
    },
    params: { param: () => undefined, display: () => '', error: () => '', isDefault: () => true, set: () => undefined, reset: () => undefined },
  })

  it('reads the markers from a manifest query', () => {
    const seen: string[] = []
    const w = render({ id: 'c', type: 'lineChart', query: 'series', annotations: { query: 'events' } }, runtime(seen))
    expect(seen).toEqual(expect.arrayContaining(['series', 'events']))
    expect(markers(w)).toHaveLength(1)
    w.unmount()
  })

  it('never runs a raw sql annotation binding', () => {
    const run = vi.fn()
    setApp({ RunA2UIQuery: run, RunA2UIQueryIn: run })
    const w = render({ id: 'c', type: 'columnChart', query: 'series', annotations: { sql: 'SELECT 1' } }, runtime())
    expect(run).not.toHaveBeenCalled()
    expect(markers(w)).toHaveLength(0)
    expect(w.find('[data-testid="a2ui-annotation-note"]').text()).toContain('only use its own named queries')
    w.unmount()
  })
})

// jsdom has no layout: give the columns the geometry a narrow, scrolling plot has
// (24px columns, 4px gaps, 100px showing) so markers must follow their columns.
describe('columnChart that scrolls sideways', () => {
  const proto = HTMLElement.prototype as any
  const saved: Record<string, PropertyDescriptor | undefined> = {}
  const stub = (name: string, get: (this: HTMLElement) => number) => {
    saved[name] = Object.getOwnPropertyDescriptor(proto, name)
    Object.defineProperty(proto, name, { configurable: true, get })
  }
  beforeEach(() => {
    stub('offsetLeft', function (this: HTMLElement) { return this.classList.contains('a2ui-columns__col') ? Array.from(this.parentElement!.children).indexOf(this) * 28 : 0 })
    stub('offsetWidth', function (this: HTMLElement) { return this.classList.contains('a2ui-columns__col') ? 24 : 0 })
    stub('scrollWidth', function (this: HTMLElement) { return this.classList.contains('a2ui-columns') ? this.children.length * 28 - 4 : 0 })
    stub('clientWidth', function (this: HTMLElement) { return this.classList.contains('a2ui-columns') ? 100 : 0 })
    stub('offsetHeight', function (this: HTMLElement) { return this.classList.contains('a2ui-columns') ? 190 : 0 })
    stub('clientHeight', function (this: HTMLElement) { return this.classList.contains('a2ui-columns') ? 175 : 0 })
  })
  afterEach(() => {
    for (const [k, d] of Object.entries(saved)) {
      if (d) Object.defineProperty(proto, k, d)
      else delete proto[k]
    }
  })

  const weeks52 = Array.from({ length: 52 }, (_, i) => [new Date(Date.UTC(2025, 0, 6 + 7 * i)).toISOString().slice(0, 10), i + 1])

  it('puts a marker at its column, not at a fraction of what shows, and moves it with the scroll', async () => {
    const w = render({ id: 'c', type: 'columnChart', columns: ['week', 'v'], rows: weeks52, annotations: { rows: [{ at: weeks52[25][0] as string, label: 'Week 26' }] } })
    await flushPromises()
    const m = markers(w)[0]
    // centre of column 25 in 52*28-4 = 1452px of content
    expect(m.element.getAttribute('style')).toContain(`left: ${((25 * 28 + 12) / 1452) * 100}`)
    const inner = w.find('.a2ui-annots__inner')
    expect(inner.attributes('style')).toContain('width: 1452px')
    expect(inner.attributes('style')).toContain('left: 0px')
    const cols = w.find('.a2ui-columns').element as HTMLElement
    cols.scrollLeft = 640
    await w.find('.a2ui-columns').trigger('scroll')
    await new Promise((r) => requestAnimationFrame(() => r(0)))
    expect(w.find('.a2ui-annots__inner').attributes('style')).toContain('left: -640px')
    expect(w.find('.a2ui-annots__clip').classes()).toContain('a2ui-annots__clip--scroll')
    // a scroll moves the layer only: the marker's own position is not recomputed
    expect(markers(w)[0].element.getAttribute('style')).toContain(`left: ${((25 * 28 + 12) / 1452) * 100}`)
    w.unmount()
  })

  it('takes the horizontal scrollbar height from the gridlines, axis and rules', async () => {
    const w = render({ id: 'c', type: 'columnChart', columns: ['week', 'v'], rows: weeks52, annotations: { rows: [{ at: weeks52[3][0] as string, label: 'x' }] } })
    await flushPromises()
    // 190 tall box, 175 of it content: the scrollbar is 15px
    expect(w.find('.a2ui-colplot').attributes('style')).toContain('--a2ui-sbh: 15px')
    expect(w.find('[data-testid="a2ui-annotations"]').attributes('style')).toContain('var(--a2ui-sbh)')
    w.unmount()
  })

  it('shows a marker tooltip outside the clip box', async () => {
    const w = render({ id: 'c', type: 'columnChart', columns: ['week', 'v'], rows: weeks52, annotations: { rows: [{ at: weeks52[3][0] as string, label: 'A long event label that must not be cut' }] } })
    await flushPromises()
    await markers(w)[0].trigger('mouseenter')
    const tip = w.find('[data-testid="a2ui-annotation-tip"]')
    expect(tip.exists()).toBe(true)
    expect(w.find('.a2ui-annots__clip').find('[data-testid="a2ui-annotation-tip"]').exists()).toBe(false)
    w.unmount()
  })
})
