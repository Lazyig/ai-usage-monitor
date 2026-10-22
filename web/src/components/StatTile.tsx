import { Caption1, Card } from '@fluentui/react-components'
import type { ReactNode } from 'react'

interface StatTileProps {
  label: string
  value: ReactNode
  hint?: ReactNode
  title?: string
  warn?: boolean
}

export function StatTile({ label, value, hint, title, warn }: StatTileProps) {
  return (
    <Card size="large" className={`stat-tile${warn ? ' stat-tile-warn' : ''}`} title={title}>
      <Caption1 className="text-fg2">{label}</Caption1>
      <div className="text-600 leading-600 font-semibold tabular-nums">{value}</div>
      {hint && <Caption1 className="text-fg3">{hint}</Caption1>}
    </Card>
  )
}
