import { Component, type ReactNode } from 'react'

interface ErrorBoundaryProps {
  children: ReactNode
  /** Prefix shown before the error message, e.g. "차트를 표시할 수 없습니다". */
  label?: string
}

interface ErrorBoundaryState {
  error: Error | null
  /** Bumped on retry and used as a key on the children wrapper so retrying
   * actually remounts the subtree (re-running effects / refetching data)
   * instead of just re-rendering the same broken tree. */
  retryCount: number
}

/**
 * Catches render-time exceptions in its subtree so a single bad widget (or
 * page) shows an inline error instead of taking down the whole app with a
 * white screen. Pair with a `key` on the boundary itself (e.g. the route
 * pathname) to also reset it on navigation.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null, retryCount: 0 }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { error }
  }

  componentDidCatch(error: Error, info: { componentStack?: string }) {
    // eslint-disable-next-line no-console
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  private retry = () => {
    this.setState((s) => ({ error: null, retryCount: s.retryCount + 1 }))
  }

  render() {
    if (this.state.error) {
      return (
        <div className="state-panel state-error" role="alert">
          <p>
            {this.props.label ?? '문제가 발생했습니다'}: {this.state.error.message || String(this.state.error)}
          </p>
          <button type="button" className="btn" onClick={this.retry}>
            다시 시도
          </button>
        </div>
      )
    }
    return <div key={this.state.retryCount}>{this.props.children}</div>
  }
}
