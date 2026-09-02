import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { labelForKey } from '../lib/colors'

interface ChartTooltipProps extends TooltipProps<ValueType, NameType> {
  formatValue: (v: number) => string
  /** Resolves a series key to its display name. Defaults to the source label
   * map; per-device series pass a resolver so UUID keys read as device names. */
  labelFor?: (key: string) => string
}

/** Themed tooltip shared by every recharts chart in the app. */
export function ChartTooltip({ active, payload, label, formatValue, labelFor = labelForKey }: ChartTooltipProps) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <div
      style={{
        background: 'var(--surface-raised)',
        border: '1px solid var(--border)',
        borderRadius: 6,
        padding: '8px 10px',
        fontSize: 12.5,
        boxShadow: 'var(--shadow)',
      }}
    >
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {payload.map((p) => (
        <div key={String(p.dataKey)} style={{ display: 'flex', gap: 8, justifyContent: 'space-between', color: 'var(--text-primary)' }}>
          <span>
            <span
              style={{
                display: 'inline-block',
                width: 8,
                height: 8,
                borderRadius: 2,
                background: p.color,
                marginRight: 6,
              }}
            />
            {labelFor(String(p.dataKey))}
          </span>
          <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatValue(Number(p.value))}</span>
        </div>
      ))}
    </div>
  )
}
