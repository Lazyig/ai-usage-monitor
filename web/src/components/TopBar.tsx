import { useEffect, useRef, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { PRESET_LABELS, TIMEZONE, addDays, type RangePreset } from '../lib/dateRange'
import { useAuth } from '../state/AuthContext'
import { useFilters } from '../state/FiltersContext'

const PRESETS: RangePreset[] = ['today', '7d', '30d', 'thisMonth', 'lastMonth', 'all', 'custom']

// Two groups: usage analytics (left) and administration (right). 기기 usage and
// 기기 관리 share a noun but not a purpose, so they are separate entries.
const NAV_ITEMS = [
  { to: '/', label: '개요', end: true },
  { to: '/agents', label: '에이전트' },
  { to: '/models', label: '모델' },
  { to: '/projects', label: '프로젝트' },
  { to: '/devices', label: '기기' },
  { divider: true },
  { to: '/manage/devices', label: '기기 관리' },
  { to: '/events', label: '이벤트' },
  { to: '/pricing', label: '가격표' },
  { to: '/downloads-page', label: '다운로드' },
] as const

export function TopBar() {
  const { range, setPreset, setCustomRange, autoRefresh, toggleAutoRefresh, refresh } = useFilters()
  const { logout } = useAuth()
  const [menuOpen, setMenuOpen] = useState(false)
  const { pathname: drawerPath } = useLocation()
  // The drawer is a phone-only affordance: close it whenever the route changes
  // and on Escape, and keep the page from scrolling underneath while it is open.
  useEffect(() => setMenuOpen(false), [drawerPath])
  useEffect(() => {
    if (!menuOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMenuOpen(false)
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [menuOpen])
  const location = useLocation()
  const navRef = useRef<HTMLElement>(null)
  const [customFrom, setCustomFrom] = useState(range.from)
  const [customTo, setCustomTo] = useState(addDays(range.to, -1))

  // Below 640px .nav becomes a horizontally-scrolling strip (see index.css);
  // keep the active link scrolled into view after navigating so it's never
  // left off-screen. `block: 'nearest'` avoids also scrolling the page.
  useEffect(() => {
    navRef.current?.querySelector('.nav-link.active')?.scrollIntoView({ block: 'nearest', inline: 'center' })
  }, [location.pathname])

  function handlePresetClick(preset: RangePreset) {
    if (preset === 'custom') {
      setCustomRange(customFrom, customTo)
      return
    }
    setPreset(preset)
  }

  function applyCustom() {
    setCustomRange(customFrom, customTo)
  }

  return (
    <header className="topbar">
      <div className="topbar-row topbar-brand-row">
        <button
          type="button"
          className="menu-btn"
          aria-label="메뉴 열기"
          aria-expanded={menuOpen}
          aria-controls="app-drawer"
          onClick={() => setMenuOpen(true)}
        >
          <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
            <path d="M4 7h16M4 12h16M4 17h16" />
          </svg>
        </button>
        <span className="brand">AI Usage Monitor</span>
        <div className="nav-wrap">
          <nav className="nav" ref={navRef}>
            {NAV_ITEMS.map((item, i) =>
              'divider' in item ? (
                <span key={`divider-${i}`} className="nav-divider" aria-hidden="true" />
              ) : (
              <NavLink key={item.to} to={item.to} end={'end' in item ? item.end : undefined} className={({ isActive }) => (isActive ? 'nav-link active' : 'nav-link')}>
                {item.label}
              </NavLink>
              ),
            )}
          </nav>
        </div>
        <button type="button" className="btn btn-ghost topbar-logout" onClick={logout}>
          로그아웃
        </button>
      </div>
      {menuOpen && (
        <div className="drawer-overlay" role="presentation" onClick={() => setMenuOpen(false)}>
          <aside id="app-drawer" className="drawer" role="dialog" aria-modal="true" aria-label="메뉴" onClick={(e) => e.stopPropagation()}>
            <div className="drawer-head">
              <span className="brand">AI Usage Monitor</span>
              <button type="button" className="icon-btn drawer-close" aria-label="메뉴 닫기" onClick={() => setMenuOpen(false)}>
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
                  <path d="M6 6l12 12M18 6L6 18" />
                </svg>
              </button>
            </div>
            <nav className="drawer-nav">
              <div className="drawer-group-label">통계</div>
              {NAV_ITEMS.slice(0, NAV_ITEMS.findIndex((i) => 'divider' in i)).map((item) =>
                'divider' in item ? null : (
                  <NavLink key={item.to} to={item.to} end={'end' in item ? item.end : undefined} className={({ isActive }) => (isActive ? 'drawer-link active' : 'drawer-link')}>
                    {item.label}
                  </NavLink>
                ),
              )}
              <div className="drawer-group-label">관리</div>
              {NAV_ITEMS.slice(NAV_ITEMS.findIndex((i) => 'divider' in i) + 1).map((item) =>
                'divider' in item ? null : (
                  <NavLink key={item.to} to={item.to} end={'end' in item ? item.end : undefined} className={({ isActive }) => (isActive ? 'drawer-link active' : 'drawer-link')}>
                    {item.label}
                  </NavLink>
                ),
              )}
            </nav>
            <div className="drawer-foot">
              <button type="button" className="btn" onClick={logout}>
                로그아웃
              </button>
            </div>
          </aside>
        </div>
      )}
      <div className="topbar-row topbar-controls">
        <div className="range-presets" role="group" aria-label="기간 선택">
          {PRESETS.map((p) => (
            <button
              key={p}
              type="button"
              className={`chip${range.preset === p ? ' chip-active' : ''}`}
              onClick={() => handlePresetClick(p)}
            >
              {PRESET_LABELS[p]}
            </button>
          ))}
        </div>
        {range.preset === 'custom' && (
          <div className="custom-range">
            <input type="date" value={customFrom} max={customTo} onChange={(e) => setCustomFrom(e.target.value)} />
            <span>~</span>
            <input type="date" value={customTo} min={customFrom} onChange={(e) => setCustomTo(e.target.value)} />
            <button type="button" className="btn btn-sm" onClick={applyCustom}>
              적용
            </button>
          </div>
        )}
        <span className="tz-badge" title="모든 기간 경계는 이 시간대 기준입니다">
          {TIMEZONE}
        </span>
        <div className="spacer" />
        <label className="auto-refresh-toggle">
          <input type="checkbox" checked={autoRefresh} onChange={toggleAutoRefresh} />
          자동 새로고침 (60초)
        </label>
        <button type="button" className="btn btn-sm" onClick={refresh}>
          새로고침
        </button>
      </div>
    </header>
  )
}
