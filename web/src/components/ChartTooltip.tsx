import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { TooltipRow, TooltipShell } from './CompositionChart'
import { labelForKey } from '../lib/colors'

interface ChartTooltipProps extends TooltipProps<ValueType, NameType> {
  formatValue: (v: number) => string
  /** Resolves a series key to its display name. Defaults to the source label
   * map; per-device series pass a resolver so UUID keys read as device names. */
  labelFor?: (key: string) => string
}

/** Themed tooltip shared by every trend chart in the app: one swatched line
 * per stacked series, in stack order. */
export function ChartTooltip({ active, payload, label, formatValue, labelFor = labelForKey }: ChartTooltipProps) {
  if (!active || !payload || payload.length === 0) return null
  return (
    <TooltipShell label={label} minWidth={0}>
      {payload.map((p) => (
        <TooltipRow key={String(p.dataKey)} color={p.color} label={labelFor(String(p.dataKey))}>
          {formatValue(Number(p.value))}
        </TooltipRow>
      ))}
    </TooltipShell>
  )
}
