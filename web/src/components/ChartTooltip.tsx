import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { labelForKey } from '../lib/colors'

interface ChartTooltipProps extends TooltipProps<ValueType, NameType> {
  formatValue: (v: number) => string
}

/** Themed tooltip shared by every recharts chart in the app. */
export function ChartTooltip({ active, payload, label, formatValue }: ChartTooltipProps) {
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
            {labelForKey(String(p.dataKey))}
          </span>
          <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatValue(Number(p.value))}</span>
        </div>
      ))}
    </div>
  )
}
