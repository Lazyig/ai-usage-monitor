import type { TimeseriesPoint, TimeseriesSeries } from '../api/types'

export interface MergedPoint {
  t: string
  [seriesKey: string]: number | string
}

/** Merges parallel per-series point lists into one row-per-bucket array for a stacked chart. */
export function mergeSeries(series: TimeseriesSeries[], valueFn: (p: TimeseriesPoint) => number): { data: MergedPoint[]; keys: string[] } {
  const tSet = new Set<string>()
  for (const s of series) for (const p of s.points) tSet.add(p.t)
  const ts = [...tSet].sort()
  const indexByT = new Map(ts.map((t, i) => [t, i]))
  const data: MergedPoint[] = ts.map((t) => ({ t }))

  for (const s of series) {
    for (const p of s.points) {
      const idx = indexByT.get(p.t)
      if (idx !== undefined) data[idx][s.key] = valueFn(p)
    }
  }
  for (const row of data) {
    for (const s of series) {
      if (row[s.key] === undefined) row[s.key] = 0
    }
  }
  return { data, keys: series.map((s) => s.key) }
}

export function tokensOfPoint(p: TimeseriesPoint): number {
  return p.input + p.output + p.cache_read + p.cache_write
}

export function costOfPoint(p: TimeseriesPoint): number {
  return p.cost_usd ?? 0
}

/** Short day label for the x-axis, e.g. "09/02" or hour "14:00". */
export function formatBucketLabel(t: string): string {
  if (t.length > 10) {
    // hour bucket: YYYY-MM-DDTHH:00:00Z
    return t.slice(11, 16)
  }
  return t.slice(5) // MM-DD
}
