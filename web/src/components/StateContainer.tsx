import type { ReactNode } from 'react'
import { Alert } from './ui/alert'
import { Button } from './ui/button'
import { Spinner } from './ui/spinner'

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
      <div className="flex items-center justify-center rounded-lg border border-dashed border-stroke2 px-4 py-8 text-center text-fg3 [&_p]:m-0 [&_p]:text-inherit" role="status">
        <Spinner label="불러오는 중…" className="text-fg1" />
      </div>
    )
  }
  if (error) {
    return (
      <Alert
        className="mx-0 my-1"
        actions={
          onRetry && (
            <Button size="small" onClick={onRetry}>
              다시 시도
            </Button>
          )
        }
      >
        데이터를 불러오지 못했습니다: {error}
      </Alert>
    )
  }
  if (empty) {
    return (
      <div className="flex items-center justify-center rounded-lg border border-dashed border-stroke2 px-4 py-8 text-center text-fg3 [&_p]:m-0 [&_p]:text-inherit">
        <p>{emptyMessage ?? '해당 기간에 데이터가 없습니다.'}</p>
      </div>
    )
  }
  return <>{children}</>
}
