import { describe, expect, it } from 'vitest'
import type { TimeseriesPoint, TimeseriesSeries } from '../api/types'
import { bucketsBetween, chartBuckets, costOfPoint, kstHourNow, mergeSeries } from './chartData'
import type { DateRange } from './dateRange'
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
})

describe('chartBuckets + mergeSeries', () => {
  const now = '2026-10-21T12:00:00+09:00'

  it('puts idle days back as zero rows', () => {
    const s = [series('claude_code', ['2026-09-24', '2026-09-27'])]
    const buckets = chartBuckets(s, { range: range('7d', '2026-09-24', '2026-09-28'), bucket: 'day' }, now)
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
    expect(chartBuckets(utc, { range: range('today', '2026-10-21', '2026-10-22'), bucket: 'hour' }, now)).toBeUndefined()
    const outside = [series('codex', ['2026-09-01'])]
    expect(chartBuckets(outside, { range: range('7d', '2026-10-15', '2026-10-22'), bucket: 'day' }, now)).toBeUndefined()
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
