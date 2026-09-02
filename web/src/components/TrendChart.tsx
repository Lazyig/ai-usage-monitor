import { Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { TimeseriesSeries } from '../api/types'
import { ChartTooltip } from './ChartTooltip'
import { costOfPoint, formatBucketLabel, mergeSeries, tokensOfPoint } from '../lib/chartData'
import { labelForKey } from '../lib/colors'
import { formatCompact, formatUsd } from '../lib/format'

interface TrendChartProps {
  series: TimeseriesSeries[]
  mode: 'cost' | 'tokens'
  /** Series key -> display name. Device series key on UUID, so those pages
   * pass a resolver; the default is the source label map. */
  labelFor?: (key: string) => string
  /** Series key -> color. Fixed per entity, never derived from the ranking. */
  colorFor: (key: string) => string
}

/**
 * Stacked per-bucket usage over time — the shape shared by the 에이전트 and
 * 기기 trend cards. One bar per bucket, one segment per series, separated by a
 * 2px surface stroke so adjacent segments never merge into one block.
 */
export function TrendChart({ series, mode, labelFor = labelForKey, colorFor }: TrendChartProps) {
  const { data, keys } = mergeSeries(series, mode === 'cost' ? costOfPoint : tokensOfPoint)
  if (data.length === 0) return null
  const format = mode === 'cost' ? formatUsd : formatCompact
  return (
    <ResponsiveContainer width="100%" height={288}>
      <BarChart data={data} barCategoryGap={4} margin={{ left: 4, right: 4 }}>
        <CartesianGrid stroke="var(--border)" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={formatBucketLabel}
          tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
          axisLine={{ stroke: 'var(--border-strong)' }}
          tickLine={false}
        />
        <YAxis
          tickFormatter={(v: number) => format(v)}
          tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
          axisLine={false}
          tickLine={false}
          width={64}
        />
        <Tooltip
          content={<ChartTooltip formatValue={format} labelFor={labelFor} />}
          cursor={{ fill: 'var(--border)', opacity: 0.4 }}
        />
        <Legend formatter={(value: string) => <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>{labelFor(value)}</span>} />
        {keys.map((key) => (
          <Bar
            key={key}
            dataKey={key}
            stackId="usage"
            fill={colorFor(key)}
            stroke="var(--surface)"
            strokeWidth={1.5}
            radius={[2, 2, 2, 2]}
            maxBarSize={28}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}
