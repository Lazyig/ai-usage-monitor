import { Badge, Button, Hamburger, Input, Switch, Tab, TabList, Tooltip } from '@fluentui/react-components'
import { ArrowClockwise20Regular } from '@fluentui/react-icons'
import { useState } from 'react'
import { PRESET_LABELS, TIMEZONE, addDays, type RangePreset } from '../lib/dateRange'
import { useFilters } from '../state/FiltersContext'

const PRESETS: RangePreset[] = ['today', '7d', '30d', 'thisMonth', 'lastMonth', 'all', 'custom']

interface TopBarProps {
  /** Phones have no docked nav pane, so the bar carries the button that opens it. */
  onOpenNav?: () => void
}

/** Command bar above every page: the date range, its time zone and refresh controls. */
export function TopBar({ onOpenNav }: TopBarProps) {
  const { range, setPreset, setCustomRange, autoRefresh, toggleAutoRefresh, refresh } = useFilters()
  const [customFrom, setCustomFrom] = useState(range.from)
  const [customTo, setCustomTo] = useState(addDays(range.to, -1))

  function handlePresetSelect(preset: RangePreset) {
    if (preset === 'custom') {
      setCustomRange(customFrom, customTo)
      return
    }
    setPreset(preset)
  }

  return (
    <header className="sticky top-0 z-10 flex flex-col gap-2 bg-[color-mix(in_srgb,var(--app-bg)_82%,transparent)] px-9 pt-[max(12px,env(safe-area-inset-top))] pb-2.5 backdrop-blur-[24px] backdrop-saturate-[1.4] max-tablet:pt-[max(8px,env(safe-area-inset-top))] max-tablet:pr-[max(16px,env(safe-area-inset-right))] max-tablet:pb-2 max-tablet:pl-[max(16px,env(safe-area-inset-left))]">
      {onOpenNav && (
        <div className="flex items-center gap-2">
          <Hamburger onClick={onOpenNav} aria-label="메뉴 열기" />
          <span className="text-400 font-semibold">AI Usage Monitor</span>
          <div className="flex-1" />
          <Button appearance="subtle" icon={<ArrowClockwise20Regular />} onClick={refresh} aria-label="새로고침" title="새로고침" />
        </div>
      )}
      <div className="flex flex-wrap items-center gap-3 max-tablet:gap-2">
        <div className="range-presets max-w-full min-w-0 overflow-x-auto [scrollbar-width:none] max-phone:w-full [&::-webkit-scrollbar]:hidden">
          <TabList
            size="small"
            appearance="subtle-circular"
            aria-label="기간 선택"
            selectedValue={range.preset}
            onTabSelect={(_, data) => handlePresetSelect(data.value as RangePreset)}
          >
            {PRESETS.map((p) => (
              <Tab key={p} value={p}>
                {PRESET_LABELS[p]}
              </Tab>
            ))}
          </TabList>
        </div>
        {range.preset === 'custom' && (
          <div className="custom-range flex items-center gap-1.5 max-phone:w-full max-phone:flex-wrap">
            <Input size="small" type="date" value={customFrom} max={customTo} onChange={(_, d) => setCustomFrom(d.value)} aria-label="시작일" />
            <span>~</span>
            <Input size="small" type="date" value={customTo} min={customFrom} onChange={(_, d) => setCustomTo(d.value)} aria-label="종료일" />
            <Button size="small" appearance="primary" onClick={() => setCustomRange(customFrom, customTo)}>
              적용
            </Button>
          </div>
        )}
        <Tooltip content="모든 기간 경계는 이 시간대 기준입니다" relationship="description">
          <Badge appearance="outline" color="informative" shape="rounded">
            {TIMEZONE}
          </Badge>
        </Tooltip>
        <div className="flex-1 max-phone:hidden" />
        <Switch checked={autoRefresh} onChange={toggleAutoRefresh} label="자동 새로고침 (60초)" />
        {/* Without the docked pane the refresh button lives in the brand row instead. */}
        {!onOpenNav && (
          <Button icon={<ArrowClockwise20Regular />} onClick={refresh}>
            새로고침
          </Button>
        )}
      </div>
    </header>
  )
}
