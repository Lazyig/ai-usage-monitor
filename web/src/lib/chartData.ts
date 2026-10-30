// Series shaping and the metric/bucket vocabulary shared by every trend chart.
// Pages fetch and derive; everything about *how* a metric turns into numbers,
// labels and toggle options lives here so 개요 / 에이전트 / 모델 / 기기 cannot
// drift apart.
import type { TimeseriesBucket, TimeseriesPoint, TimeseriesSeries } from '../api/types'
import { type DateRange, TIMEZONE, addDays, addMonths, hourlyAllowed, kstToday, mondayOf, rangeDays } from './dateRange'
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

/** The buckets a trend chart can be drawn in. The server answers hour and
 * day; a week (Monday first) or a month is folded from the daily series in
 * the browser, so switching between 일별/주별/월별 needs no new request. */
export type ChartBucket = TimeseriesBucket | 'week' | 'month'

const BUCKET_ORDER: ChartBucket[] = ['hour', 'day', 'week', 'month']
const BUCKET_LABELS: Record<ChartBucket, string> = { hour: '시간별', day: '일별', week: '주별', month: '월별' }

/** A week or month toggle needs at least this many bars to show a trend;
 * below it (이번 주 by weeks, 최근 30일 by months) the option is hidden. */
const MIN_FOLDED_BUCKETS = 3

/** Automatic bucket by how many days the chart spans: hours for a day or two,
 * days up to about a quarter, weeks up to about a year, months beyond. */
const AUTO_HOUR_MAX_DAYS = 2
const AUTO_DAY_MAX_DAYS = 92
const AUTO_WEEK_MAX_DAYS = 371

/** The days a trend chart covers, `to` exclusive: the range up to today, and
 * for "전체" from the first day with data instead of the preset's far-off start. */
export function chartSpan(range: DateRange, series: TimeseriesSeries[] | undefined, today = kstToday()): { from: string; to: string } {
  const tomorrow = addDays(today, 1)
  const to = range.to < tomorrow ? range.to : tomorrow
  let from = range.from
  if (range.preset === 'all' && series) {
    let first = ''
    for (const s of series) for (const p of s.points) if (first === '' || p.t < first) first = p.t
    if (first) from = first.slice(0, 10)
  }
  return { from, to }
}

/** How many `bucket`s the days [from, to) touch. */
function bucketCount(span: { from: string; to: string }, bucket: ChartBucket): number {
  const days = rangeDays(span.from, span.to)
  const last = addDays(span.from, days - 1)
  switch (bucket) {
    case 'hour':
      return days * 24
    case 'day':
      return days
    case 'week':
      return rangeDays(mondayOf(span.from), mondayOf(last)) / 7 + 1
    case 'month':
      return (Number(last.slice(0, 4)) - Number(span.from.slice(0, 4))) * 12 + Number(last.slice(5, 7)) - Number(span.from.slice(5, 7)) + 1
  }
}

/**
 * The bucket a trend chart is drawn in, and the options its 시간별/일별/주별/월별
 * toggle offers. `choice` is what the viewer picked (null: automatic by the
 * span). A pick the range cannot show — hours over a month, months within one
 * — falls back to the automatic one. Hours need the range itself to be short,
 * since they are fetched before the data is known; `series` (the fetched one)
 * only narrows "전체" to the days that have data.
 */
export function resolveChartBucket(
  choice: ChartBucket | null,
  range: DateRange,
  series?: TimeseriesSeries[],
  today = kstToday(),
): { bucket: ChartBucket; options: { value: ChartBucket; label: string }[] } {
  const span = chartSpan(range, series, today)
  const allowed = BUCKET_ORDER.filter((b) =>
    b === 'hour' ? hourlyAllowed(range) : b === 'day' || bucketCount(span, b) >= MIN_FOLDED_BUCKETS,
  )
  const days = rangeDays(span.from, span.to)
  const auto: ChartBucket =
    days <= AUTO_HOUR_MAX_DAYS ? 'hour' : days <= AUTO_DAY_MAX_DAYS ? 'day' : days <= AUTO_WEEK_MAX_DAYS ? 'week' : 'month'
  const pick = choice && allowed.includes(choice) ? choice : auto
  return {
    bucket: allowed.includes(pick) ? pick : 'day',
    options: allowed.map((b) => ({ value: b, label: BUCKET_LABELS[b] })),
  }
}

/** The bucket to ask the server for when the chart is drawn in `bucket`. */
export function fetchBucketFor(bucket: ChartBucket): TimeseriesBucket {
  return bucket === 'hour' ? 'hour' : 'day'
}

/** The week (its Monday, "2026-10-26") or month ("2026-10") a day falls in. */
function foldedLabel(day: string, bucket: 'week' | 'month'): string {
  return bucket === 'week' ? mondayOf(day) : day.slice(0, 7)
}

/** Sums a daily series into weeks or months. Every point field is additive,
 * so the folded totals are exactly what the server would have answered. */
export function foldSeries(series: TimeseriesSeries[], bucket: 'week' | 'month'): TimeseriesSeries[] {
  return series.map((s) => {
    const byT = new Map<string, TimeseriesPoint>()
    for (const p of s.points) {
      const t = foldedLabel(p.t.slice(0, 10), bucket)
      const acc = byT.get(t)
      if (!acc) {
        byT.set(t, { ...p, t })
        continue
      }
      acc.input += p.input
      acc.output += p.output
      acc.cache_read += p.cache_read
      acc.cache_write += p.cache_write
      acc.reasoning += p.reasoning
      acc.cost_usd = acc.cost_usd === null && p.cost_usd === null ? null : (acc.cost_usd ?? 0) + (p.cost_usd ?? 0)
      acc.events += p.events
    }
    return { ...s, points: [...byT.values()].sort((a, b) => (a.t < b.t ? -1 : a.t > b.t ? 1 : 0)) }
  })
}

export interface MergedPoint {
  t: string
  [seriesKey: string]: number | string
}

/** Seoul has no daylight saving time, so every bucket label carries this offset. */
const KST_OFFSET = '+09:00'

/**
 * Every bucket in [from, to) as the server labels them for tz=Asia/Seoul:
 * "2026-10-21" per day, "2026-10-21T14:00:00+09:00" per hour; and as
 * `foldSeries` labels them: the Monday "2026-10-19" per week, "2026-10" per
 * month (the first week or month may start before `from`). Nothing at or
 * after `now` (a KST date or hour label) is included. The server leaves empty
 * buckets out, so these are what put idle days back on the axis.
 */
export function bucketsBetween(from: string, to: string, bucket: ChartBucket, now: string): string[] {
  const out: string[] = []
  if (bucket === 'week' || bucket === 'month') {
    const today = now.slice(0, 10)
    const first = bucket === 'week' ? mondayOf(from) : addMonths(from, 0)
    const next = (d: string) => (bucket === 'week' ? addDays(d, 7) : addMonths(d, 1))
    for (let d = first; d < to && d <= today; d = next(d)) out.push(foldedLabel(d, bucket))
    return out
  }
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
  bucket: ChartBucket
}

/**
 * The buckets to draw for `window`. "전체" starts at the first bucket with data
 * instead of the preset's far-off start. Returns undefined — draw only the
 * points — when a data point does not fit the expected labels (an older
 * server without tz, or data still from a previous range during a refetch).
 * Hours over a range too long for them are such leftover data too: today's
 * hours fit 최근 90일's hourly labels, but filling those would draw thousands
 * of bars for the one render before the new days arrive.
 */
export function chartBuckets(series: TimeseriesSeries[], window: ChartWindow, now = kstHourNow()): string[] | undefined {
  const ts = series.flatMap((s) => s.points.map((p) => p.t)).sort()
  if (ts.length === 0) return undefined
  if (window.bucket === 'hour' && !hourlyAllowed(window.range)) return undefined
  // A folded week label is a Monday, a month label "YYYY-MM"; both stand for
  // the bucket's first day, which is where bucketsBetween starts too.
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

function tokensOfPoint(p: TimeseriesPoint): number {
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

/** Short x-axis label: day "10-21", hour "14:00", week "10-19~" (its
 * Monday), month "2026-10". */
export function formatBucketLabel(t: string, bucket: ChartBucket = 'day'): string {
  if (t.length > 10) {
    // hour bucket: YYYY-MM-DDTHH:00:00+09:00
    return t.slice(11, 16)
  }
  if (bucket === 'month') return t
  return bucket === 'week' ? `${t.slice(5)}~` : t.slice(5) // MM-DD
}

/** Tooltip heading for a bucket. A week reads as its days, cut to the span
 * the chart covers ("2026-10-01 ~ 10-04" for a range starting mid-week); a
 * whole month as "2026년 10월". Days and hours keep the server's label. */
export function formatBucketTitle(t: string, bucket: ChartBucket, span: { from: string; to: string }): string {
  if (bucket !== 'week' && bucket !== 'month') return t
  const first = bucket === 'week' ? t : `${t}-01`
  const last = bucket === 'week' ? addDays(t, 6) : addDays(addMonths(first, 1), -1)
  const spanLast = addDays(span.to, -1)
  const from = first < span.from ? span.from : first
  const to = last > spanLast ? spanLast : last
  if (bucket === 'month' && from === first && to === last) return `${t.slice(0, 4)}년 ${Number(t.slice(5, 7))}월`
  if (from >= to) return from
  return `${from} ~ ${to.slice(0, 4) === from.slice(0, 4) ? to.slice(5) : to}`
}
