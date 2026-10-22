// Series shaping and the metric/bucket vocabulary shared by every trend chart.
// Pages fetch and derive; everything about *how* a metric turns into numbers,
// labels and toggle options lives here so 개요 / 에이전트 / 모델 / 기기 cannot
// drift apart.
import type { TimeseriesBucket, TimeseriesPoint, TimeseriesSeries } from '../api/types'
import { type DateRange, TIMEZONE, addDays } from './dateRange'
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

/** Option list for the 일별/시간별 bucket toggle. Pages render the toggle only
 * while the range is short enough for hourly bars (see `hourlyAllowed`); on
 * wider ranges the toggle is hidden rather than shown with one dead option. */
export const BUCKET_OPTIONS: { value: TimeseriesBucket; label: string }[] = [
  { value: 'day', label: '일별' },
  { value: 'hour', label: '시간별' },
]

export interface MergedPoint {
  t: string
  [seriesKey: string]: number | string
}

/** Seoul has no daylight saving time, so every bucket label carries this offset. */
const KST_OFFSET = '+09:00'

/**
 * Every bucket in [from, to) as the server labels them for tz=Asia/Seoul:
 * "2026-10-21" per day, "2026-10-21T14:00:00+09:00" per hour. Nothing at or
 * after `now` (a KST date or hour label) is included. The server leaves empty
 * buckets out, so these are what put idle days back on the axis.
 */
export function bucketsBetween(from: string, to: string, bucket: TimeseriesBucket, now: string): string[] {
  const out: string[] = []
  for (let d = from; d < to; d = addDays(d, 1)) {
    if (bucket === 'day') {
      if (d > now.slice(0, 10)) break
      out.push(d)
      continue
    }
    for (let h = 0; h < 24; h++) {
      const t = `${d}T${String(h).padStart(2, '0')}:00:00${KST_OFFSET}`
      if (t > now) return out
      out.push(t)
    }
  }
  return out
}

/** The current KST hour as an hour-bucket label. */
export function kstHourNow(date = new Date()): string {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: TIMEZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date)
  const get = (type: string) => parts.find((p) => p.type === type)?.value ?? '00'
  return `${get('year')}-${get('month')}-${get('day')}T${get('hour')}:00:00${KST_OFFSET}`
}

/** The window a trend chart spans, so idle buckets show up as gaps. */
export interface ChartWindow {
  range: DateRange
  bucket: TimeseriesBucket
}

/**
 * The buckets to draw for `window`. "전체" starts at the first bucket with data
 * instead of the preset's far-off start. Returns undefined — draw only the
 * points — when a data point does not fit the expected labels (an older
 * server without tz, or data still from a previous range during a refetch).
 */
export function chartBuckets(series: TimeseriesSeries[], window: ChartWindow, now = kstHourNow()): string[] | undefined {
  const ts = series.flatMap((s) => s.points.map((p) => p.t)).sort()
  if (ts.length === 0) return undefined
  const from = window.range.preset === 'all' ? ts[0].slice(0, 10) : window.range.from
  const buckets = bucketsBetween(from, window.range.to, window.bucket, now)
  const known = new Set(buckets)
  return ts.every((t) => known.has(t)) ? buckets : undefined
}

/** Merges parallel per-series point lists into one row-per-bucket array for a
 * stacked chart. With `buckets`, every one of them gets a row (zero when no
 * series has a point there), so idle days stay visible as gaps. */
export function mergeSeries(
  series: TimeseriesSeries[],
  valueFn: (p: TimeseriesPoint) => number,
  buckets?: string[],
): { data: MergedPoint[]; keys: string[] } {
  const tSet = new Set<string>(buckets)
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
