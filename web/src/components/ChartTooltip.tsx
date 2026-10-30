import type { ReactNode } from 'react'
import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { labelForKey } from '../lib/colors'

interface ChartTooltipProps extends TooltipProps<ValueType, NameType> {
  formatValue: (v: number) => string
  /** Resolves a series key to its display name. Defaults to the source label
   * map; per-device series pass a resolver so UUID keys read as device names. */
  labelFor?: (key: string) => string
  /** Heading for the hovered bucket's label; the raw label when omitted. */
  formatLabel?: (label: string) => ReactNode
}

/** Themed tooltip shared by every trend chart in the app: one swatched line
 * per stacked series, in stack order. */
export function ChartTooltip({ active, payload, label, formatValue, labelFor = labelForKey, formatLabel }: ChartTooltipProps) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <TooltipShell label={formatLabel ? formatLabel(String(label)) : label} minWidth={0}>
      {payload.map((p) => (
        <TooltipRow key={String(p.dataKey)} color={p.color} label={labelFor(String(p.dataKey))}>
          {formatValue(Number(p.value))}
        </TooltipRow>
      ))}
    </TooltipShell>
  )
}

// ---- tooltip building blocks, shared with page-specific tooltips ---------

/** The themed popover every chart tooltip sits in. `minWidth` keeps a
 * multi-row tooltip from reflowing as the hovered values change width. */
export function TooltipShell({ label, minWidth = 190, children }: { label?: ReactNode; minWidth?: number; children: ReactNode }) {
  return (
    <div
      style={{
        background: 'var(--colorNeutralBackground1)',
        border: '1px solid var(--colorTransparentStroke)',
        borderRadius: 'var(--borderRadiusLarge)',
        padding: '8px 12px',
        fontSize: 12,
        boxShadow: 'var(--shadow16)',
        ...(minWidth > 0 ? { minWidth } : {}),
      }}
    >
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {children}
    </div>
  )
}

/** One `label ......... value` line, optionally with a colour swatch. */
export function TooltipRow({
  color,
  label,
  gap = 8,
  bold,
  muted,
  children,
}: {
  color?: string
  label: string
  gap?: number
  bold?: boolean
  muted?: boolean
  children: ReactNode
}) {
  return (
    <div
      style={{
        display: 'flex',
        gap,
        justifyContent: 'space-between',
        color: muted ? 'var(--text-secondary)' : 'var(--text-primary)',
        ...(bold ? { fontWeight: 600 } : {}),
      }}
    >
      <span>
        {color && (
          <span style={{ display: 'inline-block', width: 8, height: 8, borderRadius: 2, background: color, marginRight: 6 }} />
        )}
        {label}
      </span>
      <span style={{ fontVariantNumeric: 'tabular-nums' }}>{children}</span>
    </div>
  )
}

export function TooltipDivider() {
  return <div style={{ borderTop: '1px solid var(--border)', margin: '6px 0 4px' }} />
}
