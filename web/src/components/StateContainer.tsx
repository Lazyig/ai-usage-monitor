import { Button, MessageBar, MessageBarActions, MessageBarBody, Spinner } from '@fluentui/react-components'
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
      <div className="flex items-center justify-center rounded-lg border border-dashed border-stroke2 px-4 py-8 text-center text-fg3 [&_p]:m-0 [&_p]:text-inherit" role="status">
        <Spinner size="small" label="불러오는 중…" />
      </div>
    )
  }
  if (error) {
    return (
      <MessageBar intent="error" className="mx-0 my-1">
        <MessageBarBody>데이터를 불러오지 못했습니다: {error}</MessageBarBody>
        {onRetry && (
          <MessageBarActions>
            <Button size="small" onClick={onRetry}>
              다시 시도
            </Button>
          </MessageBarActions>
        )}
      </MessageBar>
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
