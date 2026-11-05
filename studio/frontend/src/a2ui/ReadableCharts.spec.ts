// Readable charts (#13 item 1): value axes with round gridlines and a zero
// baseline, a real time axis for date labels, and values reachable by hover and
// keyboard. The tick math itself is in scale.spec.ts / xaxis.spec.ts.
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import A2UIPage from './A2UIPage.vue'
import type { A2UIComponentT } from './types'

function render(c: A2UIComponentT) {
  return mount(A2UIPage, { props: { page: { title: 't', components: [c] } } })
}
const yLabels = (w: ReturnType<typeof render>) => w.findAll('.a2ui-linechart__axis-y').map((l) => l.text())
const xLabels = (w: ReturnType<typeof render>) => w.findAll('.a2ui-linechart__axis-x').map((l) => l.text())
const cx = (w: ReturnType<typeof render>) => w.findAll('circle.a2ui-linechart__dot').map((c) => Number(c.attributes('cx')))

describe('lineChart axes', () => {
  it('starts the value axis at zero for non-negative data and rounds the top', () => {
    const w = render({ id: 'l', type: 'lineChart', points: [{ label: 'a', value: 201 }, { label: 'b', value: 209 }, { label: 'c', value: 205 }] })
    const y = yLabels(w)
    expect(y[0]).toBe('0')
    expect(y.length).toBeGreaterThanOrEqual(3)
    expect(y.length).toBeLessThanOrEqual(6)
    expect(Number(y[y.length - 1].replace(/,/g, ''))).toBeGreaterThanOrEqual(209)
    expect(w.findAll('line.a2ui-linechart__grid')).toHaveLength(y.length)
  })

  it('zero: false fits the axis to the data', () => {
    const w = render({ id: 'l', type: 'lineChart', zero: false, points: [{ label: 'a', value: 201 }, { label: 'b', value: 209 }, { label: 'c', value: 205 }] })
    const y = yLabels(w).map((s) => Number(s.replace(/,/g, '')))
    expect(y[0]).toBeGreaterThan(0)
    expect(y[0]).toBeLessThanOrEqual(201)
  })

  it('spans negative data and marks the zero line', () => {
    const w = render({ id: 'l', type: 'lineChart', points: [{ label: 'a', value: -40 }, { label: 'b', value: 10 }, { label: 'c', value: 25 }] })
    expect(yLabels(w)).toContain('0')
    expect(Number(yLabels(w)[0].replace(/,/g, ''))).toBeLessThanOrEqual(-40)
    expect(w.findAll('.a2ui-linechart__grid--zero')).toHaveLength(1)
  })

  it('handles a single point and all-equal values without NaN', () => {
    const one = render({ id: 'l', type: 'lineChart', points: [{ label: '2025-01-01', value: 5 }] })
    expect(one.html()).not.toContain('NaN')
    const flat = render({ id: 'l', type: 'lineChart', points: [{ label: 'a', value: 7 }, { label: 'b', value: 7 }] })
    expect(flat.html()).not.toContain('NaN')
    expect(yLabels(flat)[0]).toBe('0')
  })

  it('places dated points by time, not by row, with calendar ticks', () => {
    const w = render({
      id: 'l', type: 'lineChart',
      points: [{ label: '2025-01-01', value: 1 }, { label: '2025-01-02', value: 2 }, { label: '2025-01-31', value: 3 }],
    })
    const [a, b, c] = cx(w)
    // Jan 1 -> Jan 2 is 1 day of 30; Jan 2 -> Jan 31 is 29. An index axis would put b halfway.
    expect((b - a) / (c - a)).toBeCloseTo(1 / 30, 2)
    expect(xLabels(w).every((l) => /^[A-Z][a-z]{2} \d+$/.test(l))).toBe(true)
  })

  it('daily, monthly and multi-year series get different tick units', () => {
    const daily = render({ id: 'l', type: 'lineChart', points: Array.from({ length: 5 }, (_, i) => ({ label: `2025-04-0${i + 1}`, value: i })) })
    expect(xLabels(daily)).toEqual(['Apr 1', 'Apr 2', 'Apr 3', 'Apr 4', 'Apr 5'])
    const monthly = render({ id: 'l', type: 'lineChart', points: Array.from({ length: 12 }, (_, i) => ({ label: `2025-${String(i + 1).padStart(2, '0')}`, value: i })) })
    expect(xLabels(monthly).length).toBeLessThanOrEqual(8)
    expect(xLabels(monthly).every((l) => /^(Q\d )?[A-Z]?[a-z]* ?'25$/.test(l) || /'25$/.test(l))).toBe(true)
    const yearly = render({ id: 'l', type: 'lineChart', points: ['2019-06-01', '2021-01-01', '2024-03-01', '2025-02-01'].map((label, i) => ({ label, value: i + 1 })) })
    expect(xLabels(yearly)).toEqual(['2020', '2021', '2022', '2023', '2024', '2025'])
  })

  it('draws unsorted dated rows left to right', () => {
    const w = render({ id: 'l', type: 'lineChart', points: [{ label: '2025-03-01', value: 3 }, { label: '2025-01-01', value: 1 }, { label: '2025-02-01', value: 2 }] })
    const xs = w.find('polyline').attributes('points')!.split(' ').map((p) => Number(p.split(',')[0]))
    expect(xs).toEqual([...xs].sort((p, q) => p - q))
  })

  it('shows a point\'s label and value on keyboard focus and arrow keys', async () => {
    const w = render({ id: 'l', type: 'lineChart', points: [{ label: '2025-01-01', value: 10 }, { label: '2025-02-01', value: 20 }, { label: '2025-03-01', value: 30 }] })
    const plot = w.find('[data-testid="a2ui-linechart-plot"]')
    expect(plot.attributes('tabindex')).toBe('0')
    expect(w.find('[data-testid="a2ui-linechart-tip"]').exists()).toBe(false)
    await plot.trigger('focus')
    expect(w.find('[data-testid="a2ui-linechart-tip"]').text()).toContain('2025-03-01')
    expect(w.find('[data-testid="a2ui-linechart-tip"]').text()).toContain('30')
    await plot.trigger('keydown', { key: 'ArrowLeft' })
    expect(w.find('[data-testid="a2ui-linechart-tip"]').text()).toContain('20')
    await plot.trigger('keydown', { key: 'Home' })
    expect(w.find('.a2ui-sr').text()).toBe('2025-01-01: 10')
    await plot.trigger('keydown', { key: 'Escape' })
    expect(w.find('[data-testid="a2ui-linechart-tip"]').exists()).toBe(false)
  })
})

describe('areaChart axes', () => {
  it('gets gridlines, a zero baseline and a time axis; the keyboard moves the readout', async () => {
    const w = render({ id: 'a', type: 'areaChart', columns: ['day', 'a'], rows: [['2025-04-01', 5], ['2025-04-02', 9], ['2025-04-10', 7]] })
    const y = w.findAll('.a2ui-linechart__axis-y').map((l) => l.text())
    expect(y[0]).toBe('0')
    expect(w.findAll('line.a2ui-kchart__grid')).toHaveLength(y.length)
    expect(w.findAll('.a2ui-linechart__axis-x').map((l) => l.text()).every((l) => /^Apr \d+$/.test(l))).toBe(true)
    const plot = w.find('.a2ui-plot__body')
    await plot.trigger('focus')
    expect(w.find('.a2ui-kchart__readout').text()).toContain('2025-04-10')
    await plot.trigger('keydown', { key: 'ArrowLeft' })
    expect(w.find('.a2ui-kchart__readout').text()).toContain('2025-04-02')
  })

  it('zero: false drops the zero from the axis', () => {
    const w = render({ id: 'a', type: 'areaChart', zero: false, columns: ['d', 'a'], rows: [['x', 100], ['y', 104], ['z', 102]] })
    expect(Number(w.find('.a2ui-linechart__axis-y').text().replace(/,/g, ''))).toBeGreaterThan(0)
  })
})

describe('barChart and columnChart axes', () => {
  it('a bar axis has round ticks and one gridline per tick in every track', () => {
    const w = render({ id: 'b', type: 'barChart', bars: [{ label: 'a', value: 87 }, { label: 'b', value: 40 }] })
    const ticks = w.findAll('.a2ui-baraxis .a2ui-linechart__axis-x').map((t) => t.text())
    expect(ticks[0]).toBe('0')
    expect(ticks[ticks.length - 1]).toBe('100')
    expect(w.findAll('.a2ui-bar')[0].findAll('.a2ui-bar__grid')).toHaveLength(ticks.length)
    expect(w.findAll('.a2ui-bar')[0].attributes('title')).toBe('a: 87')
  })

  it('draws a negative bar leftward from the zero line', () => {
    const w = render({ id: 'b', type: 'barChart', bars: [{ label: 'gain', value: 50 }, { label: 'loss', value: -50 }] })
    const [gain, loss] = w.findAll('.a2ui-bar__fill').map((f) => f.attributes('style')!)
    const num = (s: string, k: string) => Number(new RegExp(`${k}: ([0-9.]+)%`).exec(s)![1])
    expect(num(gain, 'left')).toBeCloseTo(50, 0)
    expect(num(loss, 'left')).toBeCloseTo(0, 0)
    expect(num(loss, 'width')).toBeCloseTo(50, 0)
    expect(w.findAll('.a2ui-bar__grid--zero').length).toBe(2)
  })

  it('a column chart has a labelled value axis topped by a round tick', () => {
    const w = render({ id: 'c', type: 'columnChart', columns: ['r', 'a'], rows: [['East', 38], ['West', 87]] })
    const y = w.findAll('.a2ui-colplot__yplot .a2ui-linechart__axis-y').map((l) => l.text())
    expect(y[0]).toBe('0')
    expect(y[y.length - 1]).toBe('100')
    expect(w.findAll('.a2ui-colplot__line')).toHaveLength(y.length)
    expect(w.findAll('.a2ui-columns__seg')[1].attributes('style')).toContain('height: 87%')
  })
})
