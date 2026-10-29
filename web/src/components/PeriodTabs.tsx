import { Checkmark20Regular } from '@fluentui/react-icons/headless/svg/checkmark'
import { MoreHorizontal20Regular } from '@fluentui/react-icons/headless/svg/more-horizontal'
import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { PRESET_LABELS, type RangePreset } from '../lib/dateRange'
import { Button } from './ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from './ui/dropdown-menu'
import { Tab, TabList } from './ui/tabs'

/** Display order of the presets. */
export const PRESETS: RangePreset[] = ['today', 'yesterday', '7d', '30d', 'thisMonth', 'lastMonth', 'all', 'custom']

/** When the bar is too narrow, presets move into the "⋯" menu in this order:
 * the least used first. 오늘 always stays, and so does the selected preset. */
const HIDE_ORDER: RangePreset[] = ['all', 'lastMonth', 'custom', 'thisMonth', 'yesterday', '30d', '7d']

/** Tailwind gap-1.5 between tabs and before the menu button. */
const GAP = 6

/** Which presets fit in width, given each tab's width and the menu button's. */
export function visiblePresets(width: number, tabWidths: Record<RangePreset, number>, moreWidth: number, selected: RangePreset, alwaysMenu = false): RangePreset[] {
  const hidden = new Set<RangePreset>()
  const fits = () => {
    const shown = PRESETS.filter((p) => !hidden.has(p))
    const tabs = shown.reduce((sum, p) => sum + tabWidths[p], 0) + GAP * (shown.length - 1)
    return tabs + (hidden.size > 0 || alwaysMenu ? GAP + moreWidth : 0) <= width
  }
  for (const p of HIDE_ORDER) {
    if (fits()) break
    if (p !== selected) hidden.add(p)
  }
  return PRESETS.filter((p) => !hidden.has(p))
}

interface PeriodTabsProps {
  value: RangePreset
  onSelect: (preset: RangePreset) => void
  /** Extra entries for the "⋯" menu (controls the bar has no room for); when
   * given, the menu is always shown. */
  extraMenu?: ReactNode
}

/**
 * The date range presets as tabs, showing as many as fit the space the bar
 * leaves them and moving the rest into a "⋯" menu. Widths are measured from a
 * hidden copy of every tab, so the split follows the real font and labels.
 */
export function PeriodTabs({ value, onSelect, extraMenu }: PeriodTabsProps) {
  const slot = useRef<HTMLDivElement>(null)
  const measure = useRef<HTMLDivElement>(null)
  const [shown, setShown] = useState<RangePreset[]>(PRESETS)

  useLayoutEffect(() => {
    const el = slot.current
    const m = measure.current
    if (!el || !m) return
    m.setAttribute('inert', '')
    const update = () => {
      const widths = {} as Record<RangePreset, number>
      m.querySelectorAll<HTMLElement>('[data-preset]').forEach((t) => {
        widths[t.dataset.preset as RangePreset] = t.getBoundingClientRect().width
      })
      const more = m.querySelector<HTMLElement>('[data-more]')?.getBoundingClientRect().width ?? 32
      // Nothing laid out (no layout engine, or the bar is hidden): show everything.
      const width = el.getBoundingClientRect().width
      if (width <= 0) return setShown(PRESETS)
      setShown(visiblePresets(width, widths, more, value, extraMenu != null))
    }
    update()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(update)
    ro.observe(el)
    ro.observe(m)
    return () => ro.disconnect()
  }, [value, extraMenu != null])

  const hidden = PRESETS.filter((p) => !shown.includes(p))
  const menu = hidden.length > 0 || extraMenu != null

  return (
    <div ref={slot} className="relative min-w-[140px] flex-1 basis-0">
      <div className="flex items-center gap-1.5">
        <TabList aria-label="기간 선택" value={value} onValueChange={(v) => onSelect(v as RangePreset)}>
          {shown.map((p) => (
            <Tab key={p} value={p}>
              {PRESET_LABELS[p]}
            </Tab>
          ))}
        </TabList>
        {menu && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="small" appearance="subtle" icon={<MoreHorizontal20Regular />} aria-label="기간 더 보기" data-testid="period-more" />
            </DropdownMenuTrigger>
            <DropdownMenuContent side="bottom" align="start">
              {hidden.map((p) => (
                <DropdownMenuItem key={p} icon={p === value ? <Checkmark20Regular /> : <span />} onSelect={() => onSelect(p)}>
                  {PRESET_LABELS[p]}
                </DropdownMenuItem>
              ))}
              {hidden.length > 0 && extraMenu != null && <DropdownMenuSeparator />}
              {extraMenu}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {/* Every tab and the menu button at their natural width, for measuring;
          the zero-size box keeps the copy from widening the page. */}
      <div aria-hidden className="pointer-events-none invisible absolute top-0 left-0 size-0 overflow-hidden">
        <div ref={measure} className="flex w-max items-center gap-1.5">
        <TabList value={value} onValueChange={() => {}}>
          {PRESETS.map((p) => (
            <Tab key={p} value={p} data-preset={p} tabIndex={-1}>
              {PRESET_LABELS[p]}
            </Tab>
          ))}
        </TabList>
          <Button size="small" appearance="subtle" icon={<MoreHorizontal20Regular />} tabIndex={-1} data-more />
        </div>
      </div>
    </div>
  )
}
