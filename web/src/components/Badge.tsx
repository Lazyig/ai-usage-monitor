import { Badge as FluentBadge, type BadgeProps as FluentBadgeProps } from '@fluentui/react-components'
import type { ReactNode } from 'react'

/** Every small inline status marker in the app. Adding a kind means adding a
 * label and a Fluent badge color here — nothing else picks its colours. */
type BadgeKind = 'estimated' | 'unknown' | 'warning' | 'stale' | 'revoked' | 'alias'

const LABELS: Record<BadgeKind, string> = {
  estimated: '추정',
  unknown: '단가 미상',
  warning: '주의',
  stale: '응답 없음',
  revoked: '폐기됨',
  alias: '별칭',
}

const COLORS: Record<BadgeKind, FluentBadgeProps['color']> = {
  estimated: 'brand',
  unknown: 'subtle',
  warning: 'warning',
  stale: 'severe',
  revoked: 'danger',
  alias: 'informative',
}

interface BadgeProps {
  kind: BadgeKind
  children?: ReactNode
  title?: string
}

export function Badge({ kind, children, title }: BadgeProps) {
  return (
    <FluentBadge appearance="tint" color={COLORS[kind]} shape="rounded" title={title} className="whitespace-nowrap">
      {children ?? LABELS[kind]}
    </FluentBadge>
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
    <FluentBadge appearance="outline" color="informative" shape="rounded">
      {osLabel(os)}
    </FluentBadge>
  )
}
