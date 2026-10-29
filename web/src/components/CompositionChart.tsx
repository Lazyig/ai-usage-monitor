import type { ReactElement } from 'react'
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { formatPercent } from '../lib/format'
import { TooltipDivider, TooltipRow, TooltipShell } from './ChartTooltip'
import { PHONE_QUERY, useMediaQuery } from '../hooks/useMediaQuery'

export interface CompositionRow {
  key: string
  label: string
  /** Extra numeric fields merged into the bar's datum. They are not drawn —
   * they exist so a custom `tooltip` can show quantities that are not
   * segments (a subset count, a share of some wider total, …). */
  extra?: Record<string, number>
  /** On phones the label is shortened to fit; a row whose label is a name
   * plus a qualifier (a model and its tool) gives the two parts here, so the
   * qualifier is kept as a short tag instead of being cut away. */
  axis?: { name: string; tag: string }
}

export interface CompositionSegment {
  key: string
  label: string
  color: string
}

interface CompositionChartProps {
  /** One horizontal bar per row, drawn top to bottom in this order. */
  rows: CompositionRow[]
  /** Stack order within each bar; also the legend order. */
  segments: CompositionSegment[]
  value: (rowKey: string, segmentKey: string) => number
  formatValue: (v: number) => string
  /** Width reserved for the row labels on the y axis. */
  labelWidth?: number
  /** Defaults to a height that gives each row a comfortable band. */
  height?: number
  maxBarSize?: number
  barRadius?: [number, number, number, number]
  /** A 2px surface-coloured stroke separates touching segments. Off for
   * charts whose bars are thin enough that it would eat the bar. */
  barStroke?: boolean
  /** Replaces the default composition tooltip (per-segment value + share +
   * total) for charts that need to report something else. */
  tooltip?: ReactElement
}

interface Datum {
  name: string
  total: number
  [segmentKey: string]: number | string
}

/**
 * Horizontal stacked bars answering "what is this made of" — one bar per row
 * (agent, device or model), one segment per member of the other dimension.
 * The default tooltip carries the exact per-segment values, the row total and
 * each segment's share of it.
 */
export function CompositionChart({
  rows,
  segments,
  value,
  formatValue,
  labelWidth = 140,
  height,
  maxBarSize = 22,
  barRadius = [2, 2, 2, 2],
  barStroke = true,
  tooltip,
}: CompositionChartProps) {
  const isPhone = useMediaQuery(PHONE_QUERY)
  const tickFontSize = isPhone ? 10 : 11
  // Row labels (agent/device/model names) can otherwise eat most of a
  // phone-width chart, leaving no room for the bars themselves.
  const effectiveLabelWidth = isPhone ? Math.min(labelWidth, 88) : labelWidth

  const axisByLabel = new Map(rows.filter((r) => r.axis).map((r) => [r.label, r.axis!]))
  const phoneTick = (v: string) => {
    const a = axisByLabel.get(String(v))
    return a ? `${shortenMiddle(a.name, Math.max(6, 14 - a.tag.length - 1))} ${a.tag}` : shortenMiddle(String(v), 14)
  }

  const data: Datum[] = rows.map((row) => {
    const datum: Datum = { name: row.label, total: 0, ...row.extra }
    for (const seg of segments) {
      const v = value(row.key, seg.key)
      datum[seg.key] = v
      datum.total += v
    }
    return datum
  })

  return (
    <ResponsiveContainer width="100%" height={height ?? Math.max(160, data.length * 46 + 24)}>
      <BarChart data={data} layout="vertical" margin={{ left: 8, right: 16 }}>
        <CartesianGrid stroke="var(--border)" horizontal={false} />
        <XAxis
          type="number"
          tickFormatter={formatValue}
          tick={{ fill: 'var(--text-muted)', fontSize: tickFontSize }}
          axisLine={false}
          tickLine={false}
          interval={isPhone ? 'preserveStartEnd' : undefined}
        />
        <YAxis
          type="category"
          dataKey="name"
          tick={{ fill: 'var(--text-secondary)', fontSize: isPhone ? 11 : 12 }}
          tickFormatter={isPhone ? phoneTick : undefined}
          axisLine={false}
          tickLine={false}
          width={effectiveLabelWidth}
        />
        <Tooltip
          content={tooltip ?? <CompositionTooltip segments={segments} formatValue={formatValue} />}
          cursor={{ fill: 'var(--border)', opacity: 0.4 }}
        />
        {segments.map((seg) => (
          <Bar
            key={seg.key}
            dataKey={seg.key}
            name={seg.label}
            stackId="composition"
            fill={seg.color}
            stroke={barStroke ? 'var(--surface)' : undefined}
            strokeWidth={barStroke ? 2 : undefined}
            radius={barRadius}
            maxBarSize={maxBarSize}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}

function CompositionTooltip({
  active,
  payload,
  label,
  segments,
  formatValue,
}: TooltipProps<ValueType, NameType> & { segments: CompositionSegment[]; formatValue: (v: number) => string }) {
  if (!active || !payload || payload.length === 0) return null
  const d = payload[0].payload as Datum
  const present = segments.filter((seg) => Number(d[seg.key]) > 0)
  return (
    <TooltipShell label={label}>
      {present.length === 0 && <div style={{ color: 'var(--text-muted)' }}>데이터 없음</div>}
      {present.map((seg) => (
        <TooltipRow key={seg.key} gap={12} color={seg.color} label={seg.label}>
          {formatValue(Number(d[seg.key]))}
          <span style={{ color: 'var(--text-muted)', marginLeft: 6 }}>
            {d.total > 0 ? formatPercent(Number(d[seg.key]) / d.total, 0) : ''}
          </span>
        </TooltipRow>
      ))}
      {present.length > 1 && (
        <>
          <TooltipDivider />
          <TooltipRow gap={12} label="합계" bold>
            {formatValue(d.total)}
          </TooltipRow>
        </>
      )}
    </TooltipShell>
  )
}

/** Swatch + label legend matching a composition chart's segment order. */
export function CompositionLegend({ segments }: { segments: CompositionSegment[] }) {
  return (
    <div className="mt-2 flex flex-wrap gap-4 text-200 text-fg2">
      {segments.map((seg) => (
        <span key={seg.key}>
          <span className="mr-1.5 inline-block size-2.5 rounded-sm align-[-1px]" style={{ background: seg.color }} />
          {seg.label}
        </span>
      ))}
    </div>
  )
}

/** "gemini-3.7-flash-control" -> "gemini-…-control": keeps both ends, which is
 * what distinguishes model names, instead of letting the axis clip the start. */
function shortenMiddle(s: string, max: number): string {
  if (s.length <= max) return s
  const head = Math.ceil((max - 1) / 2)
  const tail = Math.floor((max - 1) / 2)
  return `${s.slice(0, head)}…${s.slice(s.length - tail)}`
}
