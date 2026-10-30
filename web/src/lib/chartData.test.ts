import { describe, expect, it } from 'vitest'
import type { TimeseriesPoint, TimeseriesSeries } from '../api/types'
import {
  bucketsBetween,
  chartBuckets,
  costOfPoint,
  foldSeries,
  formatBucketLabel,
  formatBucketTitle,
  kstHourNow,
  mergeSeries,
  resolveChartBucket,
} from './chartData'
import { type DateRange, customRange, rangeForPreset } from './dateRange'
import { pricedShareLabel } from './usage'

const point = (t: string, cost: number): TimeseriesPoint => ({
  t,
  input: 1,
  output: 1,
  cache_read: 0,
  cache_write: 0,
  reasoning: 0,
  cost_usd: cost,
  events: 1,
})
const series = (key: string, ts: string[]): TimeseriesSeries => ({ key, points: ts.map((t) => point(t, 1)) }) as TimeseriesSeries
const range = (preset: DateRange['preset'], from: string, to: string): DateRange => ({ preset, from, to })

describe('bucketsBetween', () => {
  it('lists every day in [from, to) up to today', () => {
    expect(bucketsBetween('2026-09-29', '2026-10-03', 'day', '2026-10-21T10:00:00+09:00')).toEqual([
      '2026-09-29',
      '2026-09-30',
      '2026-10-01',
      '2026-10-02',
    ])
    expect(bucketsBetween('2026-10-20', '2026-10-23', 'day', '2026-10-21T10:00:00+09:00')).toEqual(['2026-10-20', '2026-10-21'])
  })

  it('labels hours the way the server does and stops at the current hour', () => {
    const hours = bucketsBetween('2026-10-21', '2026-10-22', 'hour', '2026-10-21T02:00:00+09:00')
    expect(hours).toEqual(['2026-10-21T00:00:00+09:00', '2026-10-21T01:00:00+09:00', '2026-10-21T02:00:00+09:00'])
    expect(bucketsBetween('2026-10-20', '2026-10-21', 'hour', '2026-10-22T00:00:00+09:00')).toHaveLength(24)
  })
  it('lists weeks by their Monday and months as YYYY-MM, up to today', () => {
    const now = '2026-10-21T10:00:00+09:00' // a Wednesday
    expect(bucketsBetween('2026-10-01', '2026-11-01', 'week', now)).toEqual(['2026-09-28', '2026-10-05', '2026-10-12', '2026-10-19'])
    expect(bucketsBetween('2026-08-15', '2026-11-01', 'month', now)).toEqual(['2026-08', '2026-09', '2026-10'])
    expect(bucketsBetween('2025-12-31', '2026-02-01', 'month', now)).toEqual(['2025-12', '2026-01'])
  })
})

describe('foldSeries', () => {
  it('sums days into their week and month, keeping every field', () => {
    const s: TimeseriesSeries[] = [
      { key: 'codex', points: [point('2026-10-18', 1), point('2026-10-19', 2), point('2026-10-25', 3), point('2026-11-02', 4)] },
    ]
    const weeks = foldSeries(s, 'week')[0].points
    expect(weeks.map((p) => [p.t, p.cost_usd, p.events, p.input])).toEqual([
      ['2026-10-12', 1, 1, 1],
      ['2026-10-19', 5, 2, 2],
      ['2026-11-02', 4, 1, 1],
    ])
    expect(foldSeries(s, 'month')[0].points.map((p) => [p.t, p.cost_usd])).toEqual([
      ['2026-10', 6],
      ['2026-11', 4],
    ])
    // The input is left alone.
    expect(s[0].points).toHaveLength(4)
  })

  it('stays null only while every folded day is unpriced', () => {
    const unpriced = (t: string) => ({ ...point(t, 0), cost_usd: null })
    const s: TimeseriesSeries[] = [{ key: 'x', points: [unpriced('2026-10-19'), unpriced('2026-10-20'), point('2026-10-27', 2), unpriced('2026-10-28')] }]
    expect(foldSeries(s, 'week')[0].points.map((p) => p.cost_usd)).toEqual([null, 2])
  })

  it('fills idle weeks back in through chartBuckets', () => {
    const s = foldSeries([series('codex', ['2026-09-29', '2026-10-20'])], 'week')
    const buckets = chartBuckets(s, { range: range('last-30', '2026-09-22', '2026-10-22'), bucket: 'week' }, '2026-10-21T12:00:00+09:00')
    const { data } = mergeSeries(s, costOfPoint, buckets)
    expect(data.map((r) => [r.t, r.codex])).toEqual([
      ['2026-09-21', 0],
      ['2026-09-28', 1],
      ['2026-10-05', 0],
      ['2026-10-12', 0],
      ['2026-10-19', 1],
    ])
  })
})

describe('resolveChartBucket', () => {
  const today = '2026-10-21'
  const preset = (p: DateRange['preset']) => rangeForPreset(p, today)
  const values = (r: ReturnType<typeof resolveChartBucket>) => r.options.map((o) => o.value)

  it('picks a bucket that fits the range', () => {
    expect(resolveChartBucket(null, preset('day-0'), undefined, today).bucket).toBe('hour')
    expect(resolveChartBucket(null, preset('day-1'), undefined, today).bucket).toBe('hour')
    expect(resolveChartBucket(null, preset('last-7'), undefined, today).bucket).toBe('day')
    expect(resolveChartBucket(null, preset('last-90'), undefined, today).bucket).toBe('day')
    expect(resolveChartBucket(null, customRange('2026-01-01', today), undefined, today).bucket).toBe('week')
    expect(resolveChartBucket(null, customRange('2024-01-01', today), undefined, today).bucket).toBe('month')
  })

  it('offers only buckets that make a trend', () => {
    expect(values(resolveChartBucket(null, preset('day-0'), undefined, today))).toEqual(['hour', 'day'])
    expect(values(resolveChartBucket(null, preset('last-7'), undefined, today))).toEqual(['hour', 'day'])
    expect(values(resolveChartBucket(null, preset('last-30'), undefined, today))).toEqual(['day', 'week'])
    expect(values(resolveChartBucket(null, preset('last-90'), undefined, today))).toEqual(['day', 'week', 'month'])
  })

  it('keeps a pick the range can show and drops one it cannot', () => {
    expect(resolveChartBucket('week', preset('last-90'), undefined, today).bucket).toBe('week')
    expect(resolveChartBucket('day', preset('day-0'), undefined, today).bucket).toBe('day')
    expect(resolveChartBucket('hour', preset('last-30'), undefined, today).bucket).toBe('day')
    expect(resolveChartBucket('month', preset('last-30'), undefined, today).bucket).toBe('day')
  })

  it('sizes "전체" by the days that have data', () => {
    const all = preset('all')
    // Before the data arrives the range is years long, but hours are never fetched for it.
    expect(resolveChartBucket(null, all, undefined, today).bucket).toBe('month')
    expect(resolveChartBucket(null, all, [series('codex', ['2026-10-01', '2026-10-20'])], today)).toEqual({
      bucket: 'day',
      options: [
        { value: 'day', label: '일별' },
        { value: 'week', label: '주별' },
      ],
    })
    expect(resolveChartBucket(null, all, [series('codex', ['2026-03-02'])], today).bucket).toBe('week')
    expect(resolveChartBucket(null, all, [series('codex', ['2026-10-21'])], today).bucket).toBe('day')
  })
})

describe('bucket labels', () => {
  it('formats the axis per bucket', () => {
    expect(formatBucketLabel('2026-10-21', 'day')).toBe('10-21')
    expect(formatBucketLabel('2026-10-21T14:00:00+09:00', 'hour')).toBe('14:00')
    expect(formatBucketLabel('2026-10-19', 'week')).toBe('10-19~')
    expect(formatBucketLabel('2026-10', 'month')).toBe('2026-10')
  })

  it('titles a week by its days within the span, a whole month by name', () => {
    const span = { from: '2026-10-01', to: '2026-10-22' }
    expect(formatBucketTitle('2026-09-28', 'week', span)).toBe('2026-10-01 ~ 10-04')
    expect(formatBucketTitle('2026-10-12', 'week', span)).toBe('2026-10-12 ~ 10-18')
    expect(formatBucketTitle('2026-10-19', 'week', span)).toBe('2026-10-19 ~ 10-21')
    expect(formatBucketTitle('2025-12-29', 'week', { from: '2025-01-01', to: '2026-02-01' })).toBe('2025-12-29 ~ 2026-01-04')
    expect(formatBucketTitle('2026-09', 'month', { from: '2026-08-15', to: '2026-10-22' })).toBe('2026년 9월')
    expect(formatBucketTitle('2026-10', 'month', { from: '2026-08-15', to: '2026-10-22' })).toBe('2026-10-01 ~ 10-21')
    expect(formatBucketTitle('2026-10-21', 'day', span)).toBe('2026-10-21')
  })
})

describe('chartBuckets + mergeSeries', () => {
  const now = '2026-10-21T12:00:00+09:00'

  it('puts idle days back as zero rows', () => {
    const s = [series('claude_code', ['2026-09-24', '2026-09-27'])]
    const buckets = chartBuckets(s, { range: range('last-7', '2026-09-24', '2026-09-28'), bucket: 'day' }, now)
    const { data } = mergeSeries(s, costOfPoint, buckets)
    expect(data.map((r) => [r.t, r.claude_code])).toEqual([
      ['2026-09-24', 1],
      ['2026-09-25', 0],
      ['2026-09-26', 0],
      ['2026-09-27', 1],
    ])
  })

  it('starts "전체" at the first day with data, not the preset start', () => {
    const s = [series('codex', ['2026-10-19', '2026-10-21'])]
    const buckets = chartBuckets(s, { range: range('all', '2020-01-01', '2026-10-22'), bucket: 'day' }, now)
    expect(buckets).toEqual(['2026-10-19', '2026-10-20', '2026-10-21'])
  })

  it('draws only the points when one does not fit the expected labels', () => {
    const utc = [series('codex', ['2026-10-21T03:00:00Z'])]
    expect(chartBuckets(utc, { range: range('day-0', '2026-10-21', '2026-10-22'), bucket: 'hour' }, now)).toBeUndefined()
    const outside = [series('codex', ['2026-09-01'])]
    expect(chartBuckets(outside, { range: range('last-7', '2026-10-15', '2026-10-22'), bucket: 'day' }, now)).toBeUndefined()
  })
})

describe('kstHourNow', () => {
  it('is the Seoul wall-clock hour', () => {
    expect(kstHourNow(new Date('2026-10-21T16:30:00Z'))).toBe('2026-10-22T01:00:00+09:00')
  })
})

describe('pricedShareLabel', () => {
  it('rounds down so a partly priced row never reads 100%', () => {
    expect(pricedShareLabel(0.9996)).toBe('가격 반영 99.9%')
    expect(pricedShareLabel(0.5)).toBe('가격 반영 50.0%')
  })
})
