import {
  AppItemStatic,
  NavDivider,
  NavDrawer,
  NavDrawerBody,
  NavDrawerFooter,
  NavDrawerHeader,
  NavItem,
  NavSectionHeader,
} from '@fluentui/react-components'
import {
  ArrowDownload20Filled,
  ArrowDownload20Regular,
  Bot20Filled,
  Bot20Regular,
  BrainCircuit20Filled,
  BrainCircuit20Regular,
  bundleIcon,
  DataUsage24Filled,
  Desktop20Filled,
  Desktop20Regular,
  DesktopEdit20Filled,
  DesktopEdit20Regular,
  Folder20Filled,
  Folder20Regular,
  Home20Filled,
  Home20Regular,
  SignOut20Regular,
  Tag20Filled,
  Tag20Regular,
  TextBulletListLtr20Filled,
  TextBulletListLtr20Regular,
} from '@fluentui/react-icons'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../state/AuthContext'

const HomeIcon = bundleIcon(Home20Filled, Home20Regular)
const BotIcon = bundleIcon(Bot20Filled, Bot20Regular)
const ModelIcon = bundleIcon(BrainCircuit20Filled, BrainCircuit20Regular)
const FolderIcon = bundleIcon(Folder20Filled, Folder20Regular)
const DesktopIcon = bundleIcon(Desktop20Filled, Desktop20Regular)
const ManageIcon = bundleIcon(DesktopEdit20Filled, DesktopEdit20Regular)
const EventsIcon = bundleIcon(TextBulletListLtr20Filled, TextBulletListLtr20Regular)
const PricingIcon = bundleIcon(Tag20Filled, Tag20Regular)
const DownloadIcon = bundleIcon(ArrowDownload20Filled, ArrowDownload20Regular)

// Two groups: usage analytics and administration. 기기 usage and 기기 관리 share
// a noun but not a purpose, so they are separate entries.
const STATS_ITEMS = [
  { to: '/', label: '개요', icon: <HomeIcon /> },
  { to: '/agents', label: '에이전트', icon: <BotIcon /> },
  { to: '/models', label: '모델', icon: <ModelIcon /> },
  { to: '/projects', label: '프로젝트', icon: <FolderIcon /> },
  { to: '/devices', label: '기기', icon: <DesktopIcon /> },
]
const ADMIN_ITEMS = [
  { to: '/manage/devices', label: '기기 관리', icon: <ManageIcon /> },
  { to: '/events', label: '이벤트', icon: <EventsIcon /> },
  { to: '/pricing', label: '가격표', icon: <PricingIcon /> },
  { to: '/downloads-page', label: '다운로드', icon: <DownloadIcon /> },
]
const LOGOUT = 'logout'

/** The nav value for a path: '/' only matches exactly, everything else by prefix. */
function selectedFor(pathname: string): string | undefined {
  return [...STATS_ITEMS, ...ADMIN_ITEMS].find(
    (item) => (item.to === '/' ? pathname === '/' : pathname === item.to || pathname.startsWith(item.to + '/')),
  )?.to
}

interface AppNavProps {
  /** Wide screens keep the pane docked (Windows Settings style); phones get an overlay. */
  inline: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function AppNav({ inline, open, onOpenChange }: AppNavProps) {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const { logout } = useAuth()

  return (
    <NavDrawer
      className={inline ? 'app-nav app-nav-docked' : 'app-nav'}
      type={inline ? 'inline' : 'overlay'}
      open={inline || open}
      onOpenChange={(_, data) => onOpenChange(data.open)}
      selectedValue={selectedFor(pathname) ?? ''}
      onNavItemSelect={(_, data) => {
        if (data.value === LOGOUT) {
          logout()
          return
        }
        navigate(data.value)
        onOpenChange(false)
      }}
    >
      <NavDrawerHeader className="app-nav-header">
        <AppItemStatic icon={<DataUsage24Filled className="text-brand" />}>AI Usage Monitor</AppItemStatic>
      </NavDrawerHeader>
      <NavDrawerBody>
        <NavSectionHeader>통계</NavSectionHeader>
        {STATS_ITEMS.map((item) => (
          <NavItem key={item.to} value={item.to} icon={item.icon}>
            {item.label}
          </NavItem>
        ))}
        <NavDivider />
        <NavSectionHeader>관리</NavSectionHeader>
        {ADMIN_ITEMS.map((item) => (
          <NavItem key={item.to} value={item.to} icon={item.icon}>
            {item.label}
          </NavItem>
        ))}
      </NavDrawerBody>
      <NavDrawerFooter>
        <NavItem value={LOGOUT} icon={<SignOut20Regular />}>
          로그아웃
        </NavItem>
      </NavDrawerFooter>
    </NavDrawer>
  )
}
