import { NavLink } from 'react-router-dom'

export interface SubTabItem {
  to: string
  label: string
  /** Match the path exactly (needed for the parent route of a nested tab). */
  end?: boolean
}

/**
 * Second-level tab strip inside a page whose routes share one top-bar entry
 * (기기 = 사용량 / 관리). Rendered as links so each tab is bookmarkable and the
 * back button moves between them.
 */
export function SubTabs({ items, ariaLabel }: { items: SubTabItem[]; ariaLabel: string }) {
  return (
    <nav className="subtabs" aria-label={ariaLabel}>
      {items.map((item) => (
        <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => (isActive ? 'subtab subtab-active' : 'subtab')}>
          {item.label}
        </NavLink>
      ))}
    </nav>
  )
}

/** The 기기 page's two tabs, shared by both of its routes. */
export const DEVICE_TABS: SubTabItem[] = [
  { to: '/devices', label: '사용량', end: true },
  { to: '/devices/manage', label: '관리' },
]
