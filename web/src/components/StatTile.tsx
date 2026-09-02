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
    <div className={`stat-tile${warn ? ' stat-tile-warn' : ''}`} title={title}>
      <div className="stat-tile-label">{label}</div>
      <div className="stat-tile-value">{value}</div>
      {hint && <div className="stat-tile-hint">{hint}</div>}
    </div>
  )
}
