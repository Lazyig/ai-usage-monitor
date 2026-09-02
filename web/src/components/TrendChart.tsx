import { Area, AreaChart, Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { TimeseriesSeries } from '../api/types'
import { ChartTooltip } from './ChartTooltip'
import { type Metric, formatBucketLabel, mergeSeries, metricFormat, metricOfPoint } from '../lib/chartData'
import { labelForKey } from '../lib/colors'

interface TrendChartProps {
  series: TimeseriesSeries[]
  mode: Metric
  /** Series key -> display name. Device series key on UUID, so those pages
   * pass a resolver; the default is the source label map. */
  labelFor?: (key: string) => string
  /** Series key -> color. Fixed per entity, never derived from the ranking;
   * `index` is the series' position for pages that colour by slot. */
  colorFor: (key: string, index: number) => string
  /** Stacked bars (the default) or a stacked area — an area reads better when
   * the point is the shape of the trend rather than each bucket's size. */
  variant?: 'bar' | 'area'
  height?: number
  /** Bar variant only: a 2px surface-coloured stroke keeps adjacent stack
   * segments from merging into one block. Off where a page's series are few
   * enough that the separators only add noise. */
  barStroke?: boolean
}

/**
 * Stacked per-bucket usage over time — the single trend chart behind the 개요,
 * 에이전트, 모델 and 기기 cards. One column (or band) per bucket, one segment
 * per series, with the shared themed tooltip and legend.
 */
export function TrendChart({ series, mode, labelFor = labelForKey, colorFor, variant = 'bar', height = 288, barStroke = true }: TrendChartProps) {
  const { data, keys } = mergeSeries(series, metricOfPoint(mode))
  if (data.length === 0) return null
  const format = metricFormat(mode)

  // The two variants share every axis, the grid, the tooltip and the legend;
  // only the chart/mark pair and the tooltip cursor differ.
  const axes = (
    <>
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
        cursor={variant === 'area' ? { stroke: 'var(--border-strong)', strokeWidth: 1 } : { fill: 'var(--border)', opacity: 0.4 }}
      />
      <Legend formatter={(value: string) => <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>{labelFor(value)}</span>} />
    </>
  )

  return (
    <ResponsiveContainer width="100%" height={height}>
      {variant === 'area' ? (
        <AreaChart data={data} margin={{ left: 4, right: 4 }}>
          {axes}
          {keys.map((key, i) => (
            <Area
              key={key}
              type="monotone"
              dataKey={key}
              stackId="usage"
              stroke={colorFor(key, i)}
              fill={colorFor(key, i)}
              fillOpacity={0.5}
              strokeWidth={2}
            />
          ))}
        </AreaChart>
      ) : (
        <BarChart data={data} barCategoryGap={4} margin={{ left: 4, right: 4 }}>
          {axes}
          {keys.map((key, i) => (
            <Bar
              key={key}
              dataKey={key}
              stackId="usage"
              fill={colorFor(key, i)}
              stroke={barStroke ? 'var(--surface)' : undefined}
              strokeWidth={barStroke ? 1.5 : undefined}
              radius={[2, 2, 2, 2]}
              maxBarSize={28}
            />
          ))}
        </BarChart>
      )}
    </ResponsiveContainer>
  )
}
