import type { ReactNode } from 'react'

interface StateContainerProps {
  loading: boolean
  error?: string | null
  empty?: boolean
  emptyMessage?: string
  onRetry?: () => void
  children: ReactNode
}

/** Wraps a data-driven section with consistent loading / error / empty states. */
export function StateContainer({ loading, error, empty, emptyMessage, onRetry, children }: StateContainerProps) {
  if (loading) {
    return (
      <div className="state-panel state-loading" role="status">
        <span className="spinner" aria-hidden="true" />
        <span>불러오는 중…</span>
      </div>
    )
  }
  if (error) {
    return (
      <div className="state-panel state-error" role="alert">
        <p>데이터를 불러오지 못했습니다: {error}</p>
        {onRetry && (
          <button type="button" className="btn" onClick={onRetry}>
            다시 시도
          </button>
        )}
      </div>
    )
  }
  if (empty) {
    return (
      <div className="state-panel state-empty">
        <p>{emptyMessage ?? '해당 기간에 데이터가 없습니다.'}</p>
      </div>
    )
  }
  return <>{children}</>
}
