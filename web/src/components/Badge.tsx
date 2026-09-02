import type { ReactNode } from 'react'

/** Every small inline status marker in the app. Adding a kind means adding a
 * `.badge-<kind>` rule in index.css — nothing else picks its colours. */
type BadgeKind = 'estimated' | 'unknown' | 'warning' | 'stale' | 'revoked' | 'alias'

const LABELS: Record<BadgeKind, string> = {
  estimated: '추정',
  unknown: '단가 미상',
  warning: '주의',
  stale: '응답 없음',
  revoked: '폐기됨',
  alias: '별칭',
}

interface BadgeProps {
  kind: BadgeKind
  children?: ReactNode
  title?: string
}

export function Badge({ kind, children, title }: BadgeProps) {
  return (
    <span className={`badge badge-${kind}`} title={title}>
      {children ?? LABELS[kind]}
    </span>
  )
}

const OS_LABELS: Record<string, string> = { linux: 'Linux', darwin: 'macOS', windows: 'Windows' }

/** Go's GOOS values as people write them. Unknown values pass through. */
export function osLabel(os: string): string {
  return OS_LABELS[os] ?? os
}

/** Neutral pill for an operating system name. */
export function OsBadge({ os }: { os: string }) {
  return <span className="tz-badge">{osLabel(os)}</span>
}
