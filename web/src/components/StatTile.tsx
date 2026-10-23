import type { ReactNode } from 'react'
import { cn } from '../lib/utils'
import { Card } from './ui/card'

interface StatTileProps {
  label: string
  value: ReactNode
  hint?: ReactNode
  title?: string
  warn?: boolean
}

export function StatTile({ label, value, hint, title, warn }: StatTileProps) {
  return (
    <Card className={cn('gap-0.5', warn && 'outline outline-offset-[-1px] outline-(--colorStatusWarningBorder2)')} title={title}>
      <span className="text-200 leading-200 text-fg2">{label}</span>
      <div className="text-600 leading-600 font-semibold tabular-nums">{value}</div>
      {hint && <span className="text-200 leading-200 text-fg3">{hint}</span>}
    </Card>
  )
}
