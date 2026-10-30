import { useState } from 'react'
import type { TimeseriesBucket, TimeseriesSeries } from '../api/types'
import { type ChartBucket, fetchBucketFor, resolveChartBucket } from '../lib/chartData'
import type { DateRange } from '../lib/dateRange'

/**
 * The 시간별/일별/주별/월별 choice of one trend card. Until the viewer picks one
 * the bucket follows the range (hours for 오늘, weeks for a long 전체 …), and a
 * pick lasts only for the range it was made in: choosing another period goes
 * back to the bucket that fits it.
 *
 * `fetchBucket` is what to ask /timeseries for; `resolve` takes the fetched
 * series and returns the bucket to draw and the toggle's options.
 */
export function useTrendBucket(range: DateRange): {
  fetchBucket: TimeseriesBucket
  resolve: (series?: TimeseriesSeries[]) => ReturnType<typeof resolveChartBucket>
  select: (bucket: ChartBucket) => void
} {
  const key = `${range.preset}|${range.from}|${range.to}`
  const [picked, setPicked] = useState<{ key: string; bucket: ChartBucket } | null>(null)
  // Forgotten as soon as the range moves, so coming back to it starts from
  // the automatic bucket again (React's "adjust state while rendering").
  if (picked && picked.key !== key) setPicked(null)
  const choice = picked?.key === key ? picked.bucket : null
  return {
    fetchBucket: fetchBucketFor(resolveChartBucket(choice, range).bucket),
    resolve: (series) => resolveChartBucket(choice, range, series),
    select: (bucket) => setPicked({ key, bucket }),
  }
}
