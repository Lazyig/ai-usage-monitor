import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { formatPercent } from '../lib/format'

export interface CompositionRow {
  key: string
  label: string
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
}

interface Datum {
  name: string
  total: number
  [segmentKey: string]: number | string
}

/**
 * Horizontal stacked bars answering "what is this made of" — one bar per row
 * (agent or device), one segment per member of the other dimension. Segments
 * are separated by a 2px surface-colored stroke rather than touching, and the
 * tooltip carries the exact per-segment values, the row total and each
 * segment's share of it.
 */
export function CompositionChart({ rows, segments, value, formatValue, labelWidth = 140 }: CompositionChartProps) {
  const data: Datum[] = rows.map((row) => {
    const datum: Datum = { name: row.label, total: 0 }
    for (const seg of segments) {
      const v = value(row.key, seg.key)
      datum[seg.key] = v
      datum.total += v
    }
    return datum
  })

  return (
    <ResponsiveContainer width="100%" height={Math.max(160, data.length * 46 + 24)}>
      <BarChart data={data} layout="vertical" margin={{ left: 8, right: 16 }}>
        <CartesianGrid stroke="var(--border)" horizontal={false} />
        <XAxis
          type="number"
          tickFormatter={formatValue}
          tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
          axisLine={false}
          tickLine={false}
        />
        <YAxis
          type="category"
          dataKey="name"
          tick={{ fill: 'var(--text-secondary)', fontSize: 12 }}
          axisLine={false}
          tickLine={false}
          width={labelWidth}
        />
        <Tooltip
          content={<CompositionTooltip segments={segments} formatValue={formatValue} />}
          cursor={{ fill: 'var(--border)', opacity: 0.4 }}
        />
        {segments.map((seg) => (
          <Bar
            key={seg.key}
            dataKey={seg.key}
            name={seg.label}
            stackId="composition"
            fill={seg.color}
            stroke="var(--surface)"
            strokeWidth={2}
            radius={[2, 2, 2, 2]}
            maxBarSize={22}
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
    <div
      style={{
        background: 'var(--surface-raised)',
        border: '1px solid var(--border)',
        borderRadius: 6,
        padding: '8px 10px',
        fontSize: 12.5,
        boxShadow: 'var(--shadow)',
        minWidth: 190,
      }}
    >
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {present.length === 0 && <div style={{ color: 'var(--text-muted)' }}>데이터 없음</div>}
      {present.map((seg) => (
        <div key={seg.key} style={{ display: 'flex', gap: 12, justifyContent: 'space-between', color: 'var(--text-primary)' }}>
          <span>
            <span
              style={{ display: 'inline-block', width: 8, height: 8, borderRadius: 2, background: seg.color, marginRight: 6 }}
            />
            {seg.label}
          </span>
          <span style={{ fontVariantNumeric: 'tabular-nums' }}>
            {formatValue(Number(d[seg.key]))}
            <span style={{ color: 'var(--text-muted)', marginLeft: 6 }}>
              {d.total > 0 ? formatPercent(Number(d[seg.key]) / d.total, 0) : ''}
            </span>
          </span>
        </div>
      ))}
      {present.length > 1 && (
        <>
          <div style={{ borderTop: '1px solid var(--border)', margin: '6px 0 4px' }} />
          <div style={{ display: 'flex', gap: 12, justifyContent: 'space-between', fontWeight: 600, color: 'var(--text-primary)' }}>
            <span>합계</span>
            <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatValue(d.total)}</span>
          </div>
        </>
      )}
    </div>
  )
}

/** Swatch + label legend matching a composition chart's segment order. */
export function CompositionLegend({ segments }: { segments: CompositionSegment[] }) {
  return (
    <div className="legend-row">
      {segments.map((seg) => (
        <span key={seg.key}>
          <span className="legend-swatch" style={{ background: seg.color }} />
          {seg.label}
        </span>
      ))}
    </div>
  )
}
