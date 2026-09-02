import type { ReactNode } from 'react'

type BadgeKind = 'estimated' | 'unknown' | 'warning' | 'stale' | 'ok' | 'revoked' | 'alias'

const LABELS: Record<BadgeKind, string> = {
  estimated: '추정',
  unknown: '단가 미상',
  warning: '주의',
  stale: '응답 없음',
  ok: '정상',
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
