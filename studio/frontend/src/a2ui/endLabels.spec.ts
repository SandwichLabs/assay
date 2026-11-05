import { describe, it, expect } from 'vitest'
import { spreadLabels } from './endLabels'

const ys = (r: { y: number }[]) => r.map((l) => l.y)

describe('spreadLabels', () => {
  it('leaves labels that already clear each other where they are', () => {
    expect(ys(spreadLabels([{ si: 0, y: 20 }, { si: 1, y: 60 }], 13, 0, 200))).toEqual([20, 60])
  })
  it('pushes overlapping labels apart by the gap', () => {
    const r = spreadLabels([{ si: 0, y: 50 }, { si: 1, y: 51 }, { si: 2, y: 50 }], 13, 0, 200)
    const y = ys(r)
    expect(y[1] - y[0]).toBeGreaterThanOrEqual(13)
    expect(y[2] - y[1]).toBeGreaterThanOrEqual(13)
  })
  it('keeps the stack inside the plot at the bottom', () => {
    const r = spreadLabels([{ si: 0, y: 198 }, { si: 1, y: 199 }, { si: 2, y: 200 }], 13, 0, 200)
    expect(r[r.length - 1].y).toBeLessThanOrEqual(200)
    expect(ys(r)[1] - ys(r)[0]).toBeGreaterThanOrEqual(13)
    expect(ys(r)[0]).toBeGreaterThanOrEqual(0)
  })
  it('spreads evenly inside the plot when the labels cannot all fit at the gap', () => {
    const r = spreadLabels(Array.from({ length: 6 }, (_, si) => ({ si, y: 50 })), 13, 10, 60)
    expect(r[0].y).toBeGreaterThanOrEqual(10)
    expect(r[5].y).toBeLessThanOrEqual(60)
    expect(ys(r)).toEqual([10, 20, 30, 40, 50, 60])
  })
  it('clamps a label outside the range and handles none', () => {
    expect(ys(spreadLabels([{ si: 0, y: -5 }], 13, 10, 190))).toEqual([10])
    expect(spreadLabels([], 13, 0, 1)).toEqual([])
  })
})
