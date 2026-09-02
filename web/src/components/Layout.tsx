import { Outlet } from 'react-router-dom'
import { TopBar } from './TopBar'

export function Layout() {
  return (
    <div className="app-shell">
      <TopBar />
      <main className="app-main">
        <Outlet />
      </main>
    </div>
  )
}
