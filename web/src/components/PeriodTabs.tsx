import { ChevronDown12Regular } from '@fluentui/react-icons/headless/svg/chevron-down'
import { MoreHorizontal20Regular } from '@fluentui/react-icons/headless/svg/more-horizontal'
import { Fragment, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { BACK, LAST_DAYS, type PeriodUnit, type RangePreset, parsePreset, presetDetail, presetLabel } from '../lib/dateRange'
import { Button } from './ui/button'
import { DropdownMenu, DropdownMenuCheckItem, DropdownMenuContent, DropdownMenuSeparator, DropdownMenuTrigger } from './ui/dropdown-menu'
import { TabButton } from './ui/tabs'

/** One entry of the bar: a tab, and for a group a ▾ menu of its other periods. */
export interface PeriodItem {
  key: PeriodUnit | 'last' | 'all' | 'custom'
  /** The tab's preset when nothing of the group is selected. */
  base: RangePreset
  /** The group's choices, in menu order; empty for a plain tab. */
  options: RangePreset[]
  /** Accessible name of the ▾ button. */
  menuLabel?: string
}

const back = (unit: PeriodUnit): RangePreset[] => Array.from({ length: BACK[unit] + 1 }, (_, n) => `${unit}-${n}` as RangePreset)

export const PERIOD_ITEMS: PeriodItem[] = [
  { key: 'day', base: 'day-0', options: back('day'), menuLabel: '지난 날짜 고르기' },
  { key: 'week', base: 'week-0', options: back('week'), menuLabel: '지난주 고르기' },
  { key: 'month', base: 'month-0', options: back('month'), menuLabel: '지난달 고르기' },
  { key: 'last', base: 'last-30', options: LAST_DAYS.map((n) => `last-${n}` as RangePreset), menuLabel: '최근 기간 고르기' },
  { key: 'all', base: 'all', options: [] },
  { key: 'custom', base: 'custom', options: [] },
]

/** A control the bar shows after the periods (the time zone, the auto
 * refresh switch) that can also move into the "⋯" menu. */
export interface BarExtra {
  key: string
  node: ReactNode
  menu: ReactNode
}

type Key = PeriodItem['key'] | string

/** When the bar is too narrow, entries move into the "⋯" menu in this order,
 * the least used first: the extras, then 전체, 최근 N일, 직접 선택, 이번 달,
 * and on the narrowest screens 이번 주 and 오늘. The item holding the selected
 * preset always stays. */
const PERIOD_HIDE_ORDER: PeriodItem['key'][] = ['all', 'last', 'custom', 'month', 'week', 'day']

/** Tailwind gap-1.5 between items and before the menu button. */
const GAP = 6

/** The item a preset belongs to. */
export function itemOf(preset: RangePreset): PeriodItem['key'] {
  return parsePreset(preset)?.kind ?? 'last'
}

/** Which entries fit in width, given each one's measured width (periods and
 * extras, extras in the order shown) and the "⋯" button's. */
export function visibleItems(width: number, widths: Record<Key, number>, moreWidth: number, selected: RangePreset, extras: string[] = []): Key[] {
  const all: Key[] = [...PERIOD_ITEMS.map((i) => i.key), ...extras]
  const hidden = new Set<Key>()
  const fits = () => {
    const shown = all.filter((k) => !hidden.has(k))
    const total = shown.reduce((sum, k) => sum + (widths[k] ?? 0), 0) + GAP * (shown.length - 1)
    return total + (hidden.size > 0 ? GAP + moreWidth : 0) <= width
  }
  for (const k of [...[...extras].reverse(), ...PERIOD_HIDE_ORDER]) {
    if (fits()) break
    if (k !== itemOf(selected)) hidden.add(k)
  }
  return all.filter((k) => !hidden.has(k))
}

interface PeriodTabsProps {
  value: RangePreset
  onSelect: (preset: RangePreset) => void
  /** Controls shown at the end of the bar while there is room, in the "⋯"
   * menu otherwise. The first one is kept longest. */
  extras?: BarExtra[]
}

function MenuEntry({ preset, value, onSelect }: { preset: RangePreset; value: RangePreset; onSelect: (p: RangePreset) => void }) {
  const detail = presetDetail(preset)
  return (
    <DropdownMenuCheckItem checked={preset === value} onSelect={() => onSelect(preset)}>
      {presetLabel(preset)}
      {detail && <span className="ml-2 text-fg3">{detail}</span>}
    </DropdownMenuCheckItem>
  )
}

/** A tab, plus the ▾ menu of a group. The tab shows the group's selected
 * period (어제, 지난주, 8월 …) or its base one. */
function Item({ item, value, onSelect, measuring }: { item: PeriodItem; value: RangePreset; onSelect: (p: RangePreset) => void; measuring?: boolean }) {
  const current = item.options.includes(value) ? value : item.base
  const tab = (
    <TabButton pressed={current === value} onClick={() => onSelect(current)} data-item={measuring && item.options.length === 0 ? item.key : undefined}>
      {presetLabel(current)}
    </TabButton>
  )
  if (item.options.length === 0) return tab
  return (
    <span className="flex items-center" data-item={measuring ? item.key : undefined}>
      {tab}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="small" appearance="subtle" icon={<ChevronDown12Regular />} aria-label={item.menuLabel} className="min-w-0 px-0.5" />
        </DropdownMenuTrigger>
        {!measuring && (
          <DropdownMenuContent side="bottom" align="start">
            {item.options.map((p) => (
              <MenuEntry key={p} preset={p} value={value} onSelect={onSelect} />
            ))}
          </DropdownMenuContent>
        )}
      </DropdownMenu>
    </span>
  )
}

/**
 * The date range selector: 오늘·이번 주·이번 달·최근 N일 with a ▾ menu each
 * for the periods before (어제, 지난주, 지난달 …), then 전체 and 직접 선택,
 * and the bar's extras at the end. When the bar is narrow, the entries that do
 * not fit move into a "⋯" menu;
 * widths are measured from a hidden copy showing the same labels, so the
 * split follows the real font and is redone when the selection renames a tab.
 */
export function PeriodTabs({ value, onSelect, extras = [] }: PeriodTabsProps) {
  const slot = useRef<HTMLDivElement>(null)
  const measure = useRef<HTMLDivElement>(null)
  const extraKeys = extras.map((e) => e.key)
  const [shown, setShown] = useState<Key[]>(() => [...PERIOD_ITEMS.map((i) => i.key), ...extraKeys])

  useLayoutEffect(() => {
    const el = slot.current
    const m = measure.current
    if (!el || !m) return
    m.setAttribute('inert', '')
    const update = () => {
      const widths: Record<Key, number> = {}
      m.querySelectorAll<HTMLElement>('[data-item]').forEach((t) => {
        widths[t.dataset.item as Key] = t.getBoundingClientRect().width
      })
      const more = m.querySelector<HTMLElement>('[data-more]')?.getBoundingClientRect().width ?? 32
      const width = el.getBoundingClientRect().width
      // Nothing laid out (no layout engine, or the bar is hidden): show everything.
      if (width <= 0) return setShown([...PERIOD_ITEMS.map((i) => i.key), ...extraKeys])
      setShown(visibleItems(width, widths, more, value, extraKeys))
    }
    update()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(update)
    ro.observe(el)
    ro.observe(m)
    return () => ro.disconnect()
    // extraKeys is derived from extras; its joined form is the stable dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, extraKeys.join()])

  // Single entries (전체, 직접 선택) before the groups' longer lists.
  const hidden = PERIOD_ITEMS.filter((i) => !shown.includes(i.key)).sort((x, y) => Number(x.options.length > 0) - Number(y.options.length > 0))
  const hiddenExtras = extras.filter((e) => !shown.includes(e.key))

  return (
    <div ref={slot} className="relative min-w-[150px] flex-1 basis-0">
      <div className="flex items-center gap-1.5">
        {/* Toggle buttons, not a tablist: each group carries its own ▾ menu. */}
        <div role="group" aria-label="기간 선택" className="flex shrink-0 items-stretch gap-1.5">
          {PERIOD_ITEMS.filter((i) => shown.includes(i.key)).map((item) => (
            <Item key={item.key} item={item} value={value} onSelect={onSelect} />
          ))}
        </div>
        {(hidden.length > 0 || hiddenExtras.length > 0) && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="small" appearance="subtle" icon={<MoreHorizontal20Regular />} aria-label="기간·설정 더 보기" />
            </DropdownMenuTrigger>
            <DropdownMenuContent side="bottom" align="start">
              {hidden.map((item, i) => (
                <Fragment key={item.key}>
                  {i > 0 && item.options.length > 0 && <DropdownMenuSeparator />}
                  {(item.options.length > 0 ? item.options : [item.base]).map((p) => (
                    <MenuEntry key={p} preset={p} value={value} onSelect={onSelect} />
                  ))}
                </Fragment>
              ))}
              {hidden.length > 0 && hiddenExtras.length > 0 && <DropdownMenuSeparator />}
              {hiddenExtras.map((e) => (
                <Fragment key={e.key}>{e.menu}</Fragment>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        {/* Pushed to the end; one flex child, so the gaps match visibleItems. */}
        {extras.some((e) => shown.includes(e.key)) && (
          <div className="ml-auto flex items-center gap-1.5">
            {extras
              .filter((e) => shown.includes(e.key))
              .map((e) => (
                <Fragment key={e.key}>{e.node}</Fragment>
              ))}
          </div>
        )}
      </div>
      {/* Every item and the menu button, for measuring; the zero-size box keeps
          the copy from widening the page. */}
      <div aria-hidden className="pointer-events-none invisible absolute top-0 left-0 size-0 overflow-hidden">
        <div ref={measure} className="flex w-max items-center gap-1.5">
          {PERIOD_ITEMS.map((item) => (
            <Item key={item.key} item={item} value={value} onSelect={() => {}} measuring />
          ))}
          {extras.map((e) => (
            <span key={e.key} data-item={e.key} className="flex">
              {e.node}
            </span>
          ))}
          <Button size="small" appearance="subtle" icon={<MoreHorizontal20Regular />} data-more />
        </div>
      </div>
    </div>
  )
}
