// Series shaping and the metric/bucket vocabulary shared by every trend chart.
// Pages fetch and derive; everything about *how* a metric turns into numbers,
// labels and toggle options lives here so 개요 / 에이전트 / 모델 / 기기 cannot
// drift apart.
import type { TimeseriesBucket, TimeseriesPoint, TimeseriesSeries } from '../api/types'
import { formatCompact, formatUsd } from './format'
import { tokensOf } from './usage'

/** The two things every usage chart can be measured in. */
export type Metric = 'cost' | 'tokens'

/** Option list for the 비용/토큰 `SegmentedControl` on every chart card. */
export const METRIC_OPTIONS: { value: Metric; label: string }[] = [
  { value: 'cost', label: '비용' },
  { value: 'tokens', label: '토큰' },
]

/** Axis/tooltip formatter for a metric. */
export function metricFormat(metric: Metric): (v: number) => string {
  return metric === 'cost' ? formatUsd : formatCompact
}

/** Point -> number extractor for a metric. */
export function metricOfPoint(metric: Metric): (p: TimeseriesPoint) => number {
  return metric === 'cost' ? costOfPoint : tokensOfPoint
}

/** Option list for the 일별/시간별 bucket toggle. Hourly is offered only on
 * short ranges (see `hourlyAllowed`), and says why when it is not. */
export function bucketOptions(hourlyAllowed: boolean, maxDays: number): {
  value: TimeseriesBucket
  label: string
  disabled?: boolean
  title?: string
}[] {
  return [
    { value: 'day', label: '일별' },
    {
      value: 'hour',
      label: '시간별',
      disabled: !hourlyAllowed,
      title: hourlyAllowed ? undefined : `시간별 보기는 기간이 ${maxDays}일 이하일 때만 쓸 수 있습니다.`,
    },
  ]
}

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
  return tokensOf(p)
}

export function costOfPoint(p: TimeseriesPoint): number {
  return p.cost_usd ?? 0
}

/** Total tokens across a series — the ranking key when a chart can only show
 * the busiest N of many series. */
export function seriesTokens(s: TimeseriesSeries): number {
  return s.points.reduce((sum, p) => sum + tokensOfPoint(p), 0)
}

/** Sorts a copy by `valueOf` descending and keeps the first `n`. Used wherever
 * a chart folds a long tail down to the entries that are actually legible. */
export function topBy<T>(items: T[], n: number, valueOf: (item: T) => number): T[] {
  return [...items].sort((a, b) => valueOf(b) - valueOf(a)).slice(0, n)
}

/** Short day label for the x-axis, e.g. "09/02" or hour "14:00". */
export function formatBucketLabel(t: string): string {
  if (t.length > 10) {
    // hour bucket: YYYY-MM-DDTHH:00:00Z
    return t.slice(11, 16)
  }
  return t.slice(5) // MM-DD
}
