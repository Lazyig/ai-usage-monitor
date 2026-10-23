import { bundleIcon, type FluentIcon } from '@fluentui/react-icons/headless'
import { ArrowDownload20Filled, ArrowDownload20Regular } from '@fluentui/react-icons/headless/svg/arrow-download'
import { Bot20Filled, Bot20Regular } from '@fluentui/react-icons/headless/svg/bot'
import { BrainCircuit20Filled, BrainCircuit20Regular } from '@fluentui/react-icons/headless/svg/brain-circuit'
import { DataUsage24Filled } from '@fluentui/react-icons/headless/svg/data-usage'
import { Desktop20Filled, Desktop20Regular } from '@fluentui/react-icons/headless/svg/desktop'
import { DesktopEdit20Filled, DesktopEdit20Regular } from '@fluentui/react-icons/headless/svg/desktop-edit'
import { Folder20Filled, Folder20Regular } from '@fluentui/react-icons/headless/svg/folder'
import { Home20Filled, Home20Regular } from '@fluentui/react-icons/headless/svg/home'
import { SignOut20Regular } from '@fluentui/react-icons/headless/svg/sign-out'
import { Tag20Filled, Tag20Regular } from '@fluentui/react-icons/headless/svg/tag'
import { TextBulletListLtr20Filled, TextBulletListLtr20Regular } from '@fluentui/react-icons/headless/svg/text-bullet-list-ltr'
import type { ComponentProps, ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { cn } from '../lib/utils'
import { useAuth } from '../state/AuthContext'
import { Sheet, SheetContent } from './ui/sheet'

// Two groups: usage analytics and administration. 기기 usage and 기기 관리 share
// a noun but not a purpose, so they are separate entries.
const STATS_ITEMS = [
  { to: '/', label: '개요', icon: bundleIcon(Home20Filled, Home20Regular) },
  { to: '/agents', label: '에이전트', icon: bundleIcon(Bot20Filled, Bot20Regular) },
  { to: '/models', label: '모델', icon: bundleIcon(BrainCircuit20Filled, BrainCircuit20Regular) },
  { to: '/projects', label: '프로젝트', icon: bundleIcon(Folder20Filled, Folder20Regular) },
  { to: '/devices', label: '기기', icon: bundleIcon(Desktop20Filled, Desktop20Regular) },
]
const ADMIN_ITEMS = [
  { to: '/manage/devices', label: '기기 관리', icon: bundleIcon(DesktopEdit20Filled, DesktopEdit20Regular) },
  { to: '/events', label: '이벤트', icon: bundleIcon(TextBulletListLtr20Filled, TextBulletListLtr20Regular) },
  { to: '/pricing', label: '가격표', icon: bundleIcon(Tag20Filled, Tag20Regular) },
  { to: '/downloads-page', label: '다운로드', icon: bundleIcon(ArrowDownload20Filled, ArrowDownload20Regular) },
]

/** The nav value for a path: '/' only matches exactly, everything else by prefix. */
function selectedFor(pathname: string): string | undefined {
  return [...STATS_ITEMS, ...ADMIN_ITEMS].find(
    (item) => (item.to === '/' ? pathname === '/' : pathname === item.to || pathname.startsWith(item.to + '/')),
  )?.to
}

// Fluent 2 nav item: fills only on hover or selection; the selected page gets
// a brand indicator bar at the start edge and its icon turns filled and brand.
const NAV_ITEM =
  'relative box-border flex w-full cursor-pointer items-start gap-4 rounded-md border-none bg-transparent py-2.5 pr-2 pl-2.5 text-left text-300 leading-300 text-fg2 no-underline transition-[background-color] duration-(--durationFaster) ease-(--curveLinear) hover:bg-(--colorSubtleBackgroundHover) hover:text-fg2 hover:no-underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-(--colorStrokeFocus2)'
const NAV_ITEM_SELECTED =
  'bg-(--colorSubtleBackgroundSelected) font-semibold hover:bg-(--colorSubtleBackgroundSelected) after:absolute after:top-2.5 after:left-[-6px] after:h-5 after:w-1 after:rounded-full after:bg-(--colorCompoundBrandForeground1) after:content-[""]'
const NAV_ICON = 'grid min-h-5 min-w-5 items-start justify-center overflow-hidden [&>svg]:col-start-1 [&>svg]:row-start-1'

function NavItem({ selected, icon: Icon, children, ...props }: { selected: boolean; icon: FluentIcon; children: ReactNode } & ComponentProps<typeof Link>) {
  return (
    <Link aria-current={selected ? 'page' : undefined} className={cn(NAV_ITEM, selected && NAV_ITEM_SELECTED)} {...props}>
      <span className={cn(NAV_ICON, selected && 'text-(--colorNeutralForeground2BrandSelected)')}>
        <Icon filled={selected} />
      </span>
      {children}
    </Link>
  )
}

function NavSectionHeader({ children }: { children: ReactNode }) {
  return <h3 className="my-2 ms-2.5 me-0 text-200 leading-200 font-semibold text-fg1">{children}</h3>
}

function NavPane({ onNavigate }: { onNavigate: () => void }) {
  const { pathname } = useLocation()
  const { logout } = useAuth()
  const selected = selectedFor(pathname)
  const item = (entry: (typeof STATS_ITEMS)[number]) => (
    <NavItem key={entry.to} to={entry.to} selected={entry.to === selected} icon={entry.icon} onClick={onNavigate}>
      {entry.label}
    </NavItem>
  )

  return (
    <>
      <div className="relative z-2 flex flex-col py-[5px] pr-6 pl-3.5">
        <div className="ms-[-6px] flex items-center gap-2.5 rounded-md py-2 pr-2 pl-2.5 text-400 leading-400 font-semibold text-fg2">
          <span className="grid size-6 min-h-5 min-w-5 place-items-center overflow-hidden">
            <DataUsage24Filled className="text-brand" />
          </span>
          AI Usage Monitor
        </div>
      </div>
      <nav aria-label="주 메뉴" className="relative z-1 flex flex-1 flex-col gap-0.5 overflow-y-auto pr-1 pl-2.5">
        <NavSectionHeader>통계</NavSectionHeader>
        {STATS_ITEMS.map(item)}
        <hr className="my-1 border-stroke1" />
        <NavSectionHeader>관리</NavSectionHeader>
        {ADMIN_ITEMS.map(item)}
      </nav>
      <div className="relative z-2 flex flex-col items-center gap-0.5">
        <button type="button" className={NAV_ITEM} onClick={logout}>
          <span className={NAV_ICON}>
            <SignOut20Regular />
          </span>
          로그아웃
        </button>
      </div>
    </>
  )
}

interface AppNavProps {
  /** Wide screens keep the pane docked (Windows Settings style); phones get an overlay. */
  inline: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function AppNav({ inline, open, onOpenChange }: AppNavProps) {
  if (inline) {
    return (
      <aside className="sticky top-0 flex h-svh w-[260px] flex-none flex-col overflow-hidden">
        <NavPane onNavigate={() => {}} />
      </aside>
    )
  }
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent title="탐색" className="w-[260px] bg-(--colorNeutralBackground4)">
        <NavPane onNavigate={() => onOpenChange(false)} />
      </SheetContent>
    </Sheet>
  )
}
