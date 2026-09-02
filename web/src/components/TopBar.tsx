import { useState } from 'react'
import { NavLink } from 'react-router-dom'
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
  const [customFrom, setCustomFrom] = useState(range.from)
  const [customTo, setCustomTo] = useState(addDays(range.to, -1))

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
        <span className="brand">AI Usage Monitor</span>
        <nav className="nav">
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
        <button type="button" className="btn btn-ghost" onClick={logout}>
          로그아웃
        </button>
      </div>
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
