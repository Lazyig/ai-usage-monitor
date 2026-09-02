import { Outlet, useLocation } from 'react-router-dom'
import { ErrorBoundary } from './ErrorBoundary'
import { TopBar } from './TopBar'

export function Layout() {
  const location = useLocation()
  return (
    <div className="app-shell">
      <TopBar />
      <main className="app-main">
        {/* Keyed on the path so navigating away from a crashed page always
         * lands on a clean boundary rather than the previous error. */}
        <ErrorBoundary key={location.pathname} label="이 페이지를 표시할 수 없습니다">
          <Outlet />
        </ErrorBoundary>
      </main>
    </div>
  )
}
