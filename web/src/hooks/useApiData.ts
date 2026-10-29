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
 *
 * Callers must list `refreshKey` (from useFilters) in `deps` for the refresh
 * button and auto refresh to re-fetch at all; a change of only that key (or a
 * `reload()`) is what keeps the current data on screen while it loads.
 */
export function useApiData<T>(fetcher: () => Promise<T>, deps: unknown[]): ApiState<T> & { reload: () => void } {
  const [state, setState] = useState<ApiState<T>>({ status: 'loading' })
  const [tick, setTick] = useState(0)
  const refreshKey = useRefreshKey()
  const last = useRef({ refreshKey, tick, deps })

  useEffect(() => {
    let cancelled = false
    // A refresh (the refresh button, auto refresh, reload()) keeps the current
    // data on screen until the new data arrives: swapping it for a spinner —
    // every minute with auto refresh on — would reset the scroll position,
    // table sorting and any open menu. Any other change (range, source, …)
    // asks a different question, so its old answer must not stay up as if it
    // were the new one.
    // Only the refresh counter (or tick) moved: every other dependency is the
    // same. Auto refresh at midnight moves the range of a "today" preset in
    // the same render, and that is a new question.
    const prev = last.current
    const isRefresh =
      deps.length === prev.deps.length &&
      deps.every((d, i) => Object.is(d, prev.deps[i]) || (d === refreshKey && prev.deps[i] === prev.refreshKey))
    last.current = { refreshKey, tick, deps }
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
