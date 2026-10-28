import { useEffect, useRef, useState } from 'react'
import { describeError } from '../lib/errors'
import { useRefreshKey } from '../state/FiltersContext'

export type ApiState<T> =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'success'; data: T }

/**
 * Runs `fetcher` whenever `deps` change, tracking loading/error/success state.
 * `fetcher` is expected to be stable-enough (usually an inline closure) —
 * only `deps` controls when it re-runs.
 */
export function useApiData<T>(fetcher: () => Promise<T>, deps: unknown[]): ApiState<T> & { reload: () => void } {
  const [state, setState] = useState<ApiState<T>>({ status: 'loading' })
  const [tick, setTick] = useState(0)
  const refreshKey = useRefreshKey()
  const last = useRef({ refreshKey, tick })

  useEffect(() => {
    let cancelled = false
    // A refresh (the refresh button, auto refresh, reload()) keeps the current
    // data on screen until the new data arrives: swapping it for a spinner —
    // every minute with auto refresh on — would reset the scroll position,
    // table sorting and any open menu. Any other change (range, source, …)
    // asks a different question, so its old answer must not stay up as if it
    // were the new one.
    const isRefresh = refreshKey !== last.current.refreshKey || tick !== last.current.tick
    last.current = { refreshKey, tick }
    setState((s) => (s.status === 'success' && isRefresh ? s : { status: 'loading' }))
    fetcher()
      .then((data) => {
        if (!cancelled) setState({ status: 'success', data })
      })
      .catch((err: unknown) => {
        if (!cancelled) setState({ status: 'error', message: describeError(err) })
      })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick])

  return { ...state, reload: () => setTick((t) => t + 1) }
}
