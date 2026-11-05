// endLabels — where the direct labels at the right end of a multi-series line
// chart sit. Each series' last value is a y position; labels that would overlap
// are pushed apart (and kept inside the plot) so every name stays readable.

export interface EndLabel {
  si: number
  y: number
}

// spreadLabels keeps at least `gap` between neighbouring labels, within [lo, hi],
// moving each as little as it can: a forward pass pushes down, then a backward
// pass pulls up whatever ran past `hi`. Input order is not significant.
export function spreadLabels(items: EndLabel[], gap: number, lo: number, hi: number): EndLabel[] {
  const out = items.map((i) => ({ ...i, y: Math.min(Math.max(i.y, lo), hi) })).sort((a, b) => a.y - b.y || a.si - b.si)
  for (let i = 1; i < out.length; i++) out[i].y = Math.max(out[i].y, out[i - 1].y + gap)
  for (let i = out.length - 1; i >= 0; i--) {
    const cap = i === out.length - 1 ? hi : out[i + 1].y - gap
    out[i].y = Math.min(out[i].y, cap)
  }
  return out
}
