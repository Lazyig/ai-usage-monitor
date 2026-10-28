import type { VariantProps } from 'class-variance-authority'
import type { ReactNode } from 'react'
import { Badge as BadgeSurface, type badgeVariants } from './ui/badge'

/** Every small inline status marker in the app. Adding a kind means adding a
 * label and a badge color here — nothing else picks its colours. */
type BadgeKind = 'estimated' | 'unknown' | 'warning' | 'stale' | 'revoked' | 'alias' | 'outdated'

const LABELS: Record<BadgeKind, string> = {
  estimated: '추정',
  unknown: '단가 미상',
  warning: '주의',
  stale: '응답 없음',
  revoked: '폐기됨',
  alias: '별칭',
  outdated: '버전 불일치',
}

const COLORS: Record<BadgeKind, VariantProps<typeof badgeVariants>['color']> = {
  estimated: 'brand',
  unknown: 'subtle',
  warning: 'warning',
  stale: 'severe',
  revoked: 'danger',
  alias: 'informative',
  outdated: 'warning',
}

interface BadgeProps {
  kind: BadgeKind
  children?: ReactNode
  title?: string
}

export function Badge({ kind, children, title }: BadgeProps) {
  return (
    <BadgeSurface color={COLORS[kind]} title={title} className="whitespace-nowrap">
      {children ?? LABELS[kind]}
    </BadgeSurface>
  )
}

const OS_LABELS: Record<string, string> = { linux: 'Linux', darwin: 'macOS', windows: 'Windows' }

/** Go's GOOS values as people write them. Unknown values pass through. */
export function osLabel(os: string): string {
  return OS_LABELS[os] ?? os
}

/** Neutral outline badge for an operating system name. */
export function OsBadge({ os }: { os: string }) {
  return (
    <BadgeSurface appearance="outline">{osLabel(os)}</BadgeSurface>
  )
}
