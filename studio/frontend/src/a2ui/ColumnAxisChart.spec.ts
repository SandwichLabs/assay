// columnChart x labels (#333): dated columns get time ticks, categories keep every label.
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import A2UIPage from './A2UIPage.vue'
import { _resetQueryPoolForTests } from './query'
import type { A2UIComponentT } from './types'

const render = (c: Record<string, unknown>) => mount(A2UIPage, { props: { page: { title: 't', components: [{ id: 'c', type: 'columnChart', columns: ['d', 'v'], ...c } as A2UIComponentT] } } })
const days = (n: number) => Array.from({ length: n }, (_, i) => new Date(Date.UTC(2025, 3, 1 + i)).toISOString().slice(0, 10))
const ticks = (w: ReturnType<typeof render>) => w.findAll('[data-testid="a2ui-column-tick"]')
const shown = (w: ReturnType<typeof render>) => ticks(w).map((t) => t.text()).filter(Boolean)

beforeEach(() => _resetQueryPoolForTests())

describe('columnChart x axis', () => {
  it('labels dated columns with time ticks and keeps the full label on hover', () => {
    const w = render({ rows: days(60).map((d, i) => [d, i + 1]) })
    expect(ticks(w)).toHaveLength(60)
    expect(shown(w).length).toBeGreaterThan(1)
    expect(shown(w).every((t) => /^[A-Z][a-z]{2} \d{1,2}$/.test(t))).toBe(true)
    const t0 = ticks(w).find((t) => t.text())!
    expect(t0.attributes('title')).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(w.find('.a2ui-columns__seg').attributes('title')).toContain('2025-04-01')
    w.unmount()
  })
  it('hides unlabelled columns from assistive tech', () => {
    const w = render({ rows: days(30).map((d, i) => [d, i + 1]) })
    expect(ticks(w).find((t) => !t.text())!.attributes('aria-hidden')).toBe('true')
    w.unmount()
  })
  it('keeps category labels as they are', () => {
    const w = render({ rows: [['north', 3], ['south', 4]] })
    expect(ticks(w)).toHaveLength(0)
    expect(w.findAll('.a2ui-columns__label').map((x) => x.text())).toEqual(['north', 'south'])
    w.unmount()
  })
  it('keeps full labels when sorted by size', () => {
    const w = render({ sorted: true, rows: days(5).map((d, i) => [d, i + 1]) })
    expect(ticks(w)).toHaveLength(0)
    expect(w.findAll('.a2ui-columns__label')).toHaveLength(5)
    w.unmount()
  })
  it('falls back to category labels for unordered dates', () => {
    const w = render({ rows: days(5).reverse().map((d, i) => [d, i + 1]) })
    expect(ticks(w)).toHaveLength(0)
    w.unmount()
  })
})
