import { useEffect, useState } from 'react'
import { describeError } from '../lib/errors'

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

  useEffect(() => {
    let cancelled = false
    setState({ status: 'loading' })
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
