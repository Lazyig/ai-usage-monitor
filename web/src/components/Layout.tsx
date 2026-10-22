import { useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { AppNav } from './AppNav'
import { ErrorBoundary } from './ErrorBoundary'
import { TopBar } from './TopBar'

/** Below this width the nav pane stops docking and opens as an overlay. */
const DOCKED_NAV_QUERY = '(min-width: 1024px)'

export function Layout() {
  const location = useLocation()
  const docked = useMediaQuery(DOCKED_NAV_QUERY)
  const [navOpen, setNavOpen] = useState(false)
  return (
    <div className="flex min-h-svh">
      <AppNav inline={docked} open={navOpen} onOpenChange={setNavOpen} />
      <div className="flex min-w-0 flex-1 flex-col">
        <TopBar onOpenNav={docked ? undefined : () => setNavOpen(true)} />
        <main className="w-full max-w-[1440px] flex-1 px-9 pt-1 pb-[max(48px,env(safe-area-inset-bottom))] max-tablet:pr-[max(16px,env(safe-area-inset-right))] max-tablet:pb-[max(32px,env(safe-area-inset-bottom))] max-tablet:pl-[max(16px,env(safe-area-inset-left))]">
          {/* Keyed on the path so navigating away from a crashed page always
           * lands on a clean boundary rather than the previous error. */}
          <ErrorBoundary key={location.pathname} label="이 페이지를 표시할 수 없습니다">
            <Outlet />
          </ErrorBoundary>
        </main>
      </div>
    </div>
  )
}
