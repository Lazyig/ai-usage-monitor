import { ArrowDownRegular } from '@fluentui/react-icons/headless/svg/arrow-down'
import { ArrowUpRegular } from '@fluentui/react-icons/headless/svg/arrow-up'
import { CalendarLtrRegular } from '@fluentui/react-icons/headless/svg/calendar-ltr'
import { Popover as PopoverPrimitive } from 'radix-ui'
import { useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react'
import { addDays, kstToday } from '@/lib/dateRange'
import { cn } from '@/lib/utils'
import { Input } from './input'

const WEEKDAYS = ['일', '월', '화', '수', '목', '금', '토']

interface DatePickerProps {
  /** YYYY-MM-DD, or '' while the field is cleared. */
  value: string
  onChange: (value: string) => void
  min?: string
  max?: string
  'aria-label': string
  size?: 'small' | 'medium'
  className?: string
}

/**
 * Fluent 2 date picker: the date field stays typeable, and the calendar
 * button opens a Fluent month calendar instead of the browser's picker, which
 * ignores the theme (it stays light in dark mode).
 */
function DatePicker({ value, onChange, min, max, size = 'medium', className, ...props }: DatePickerProps) {
  const [open, setOpen] = useState(false)
  const label = props['aria-label']
  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Anchor data-slot="date-picker" className={cn('inline-flex', className)}>
        <Input
          size={size}
          type="date"
          data-date-picker=""
          value={value}
          min={min}
          max={max}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={(e) => {
            // The browser's own shortcut for its picker opens ours instead.
            if (e.altKey && e.key === 'ArrowDown') {
              e.preventDefault()
              setOpen(true)
            }
          }}
          aria-label={label}
          className="w-full pr-0.5"
          contentAfter={
            <PopoverPrimitive.Trigger
              aria-label={`${label} 달력 열기`}
              className="inline-flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-sm text-[20px] leading-0 text-fg3 hover:text-(--colorNeutralForeground2BrandHover) focus-visible:outline-2 focus-visible:outline-(--colorStrokeFocus2) data-[state=open]:text-(--colorCompoundBrandForeground1)"
            >
              <CalendarLtrRegular />
            </PopoverPrimitive.Trigger>
          }
        />
      </PopoverPrimitive.Anchor>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          data-slot="date-picker-calendar"
          side="bottom"
          align="start"
          sideOffset={4}
          collisionPadding={8}
          aria-label={`${label} 달력`}
          onOpenAutoFocus={(e) => {
            e.preventDefault()
            ;(e.currentTarget as HTMLElement).querySelector<HTMLElement>('[tabindex="0"][data-day]')?.focus()
          }}
          className="z-50 box-border rounded-md border border-transparent bg-bg1 p-3 text-200 leading-200 text-fg1 shadow-16 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2"
        >
          <Calendar
            value={value}
            min={min}
            max={max}
            onSelect={(day) => {
              onChange(day)
              setOpen(false)
            }}
          />
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

interface CalendarProps {
  value: string
  min?: string
  max?: string
  onSelect: (day: string) => void
}

/** Fluent month grid: today in brand fill, the selected day tinted, arrow keys move by day/week, PageUp/PageDown by month. */
function Calendar({ value, min, max, onSelect }: CalendarProps) {
  const today = kstToday()
  const [focused, setFocused] = useState(value || today)
  const grid = useRef<HTMLDivElement>(null)
  const month = focused.slice(0, 7)
  const first = `${month}-01`
  const start = addDays(first, -weekday(first))
  const days = Array.from({ length: 42 }, (_, i) => addDays(start, i))
  const outOfRange = (d: string) => (min != null && min !== '' && d < min) || (max != null && max !== '' && d > max)

  const keyboardMove = useRef(false)

  function move(day: string) {
    keyboardMove.current = true
    setFocused(day)
  }

  // Focus the day once React has rendered it (it may be in a new month), before the next key.
  useLayoutEffect(() => {
    if (!keyboardMove.current) return
    keyboardMove.current = false
    grid.current?.querySelector<HTMLElement>(`[data-day="${focused}"]`)?.focus()
  }, [focused])

  function onKeyDown(e: KeyboardEvent) {
    const step: Record<string, () => string> = {
      ArrowLeft: () => addDays(focused, -1),
      ArrowRight: () => addDays(focused, 1),
      ArrowUp: () => addDays(focused, -7),
      ArrowDown: () => addDays(focused, 7),
      Home: () => addDays(focused, -weekday(focused)),
      End: () => addDays(focused, 6 - weekday(focused)),
      PageUp: () => addMonths(focused, -1),
      PageDown: () => addMonths(focused, 1),
    }
    if (step[e.key]) {
      e.preventDefault()
      move(step[e.key]())
    }
  }

  const navButton =
    'inline-flex size-6 cursor-pointer items-center justify-center rounded-sm text-[16px] leading-0 text-fg2 hover:bg-(--colorSubtleBackgroundHover) active:bg-(--colorSubtleBackgroundPressed) focus-visible:outline-2 focus-visible:outline-(--colorStrokeFocus2)'

  return (
    <div className="w-[236px]">
      <div className="mb-2 flex items-center justify-between">
        <span aria-live="polite" className="px-1 text-300 leading-300 font-semibold">
          {Number(month.slice(0, 4))}년 {Number(month.slice(5))}월
        </span>
        <span className="flex gap-1">
          <button type="button" aria-label="이전 달" className={navButton} onClick={() => setFocused(addMonths(focused, -1))}>
            <ArrowUpRegular />
          </button>
          <button type="button" aria-label="다음 달" className={navButton} onClick={() => setFocused(addMonths(focused, 1))}>
            <ArrowDownRegular />
          </button>
        </span>
      </div>
      <div role="grid" ref={grid} onKeyDown={onKeyDown} className="grid grid-cols-7 gap-y-0.5 text-center">
        {WEEKDAYS.map((w) => (
          <span key={w} role="columnheader" className="pb-1 text-fg3">
            {w}
          </span>
        ))}
        {days.map((d) => {
          const disabled = outOfRange(d)
          const inMonth = d.startsWith(month)
          const selected = d === value
          const isToday = d === today
          return (
            <button
              key={d}
              type="button"
              role="gridcell"
              data-day={d}
              tabIndex={d === focused ? 0 : -1}
              disabled={disabled}
              aria-selected={selected}
              aria-current={isToday ? 'date' : undefined}
              aria-label={`${Number(d.slice(5, 7))}월 ${Number(d.slice(8))}일`}
              onClick={() => onSelect(d)}
              // A press on a day of the previous or next month focuses it before the
              // click lands; switching months then would move the grid under the
              // pointer and lose the click. Arrow keys change month through move().
              onFocus={() => {
                if (inMonth) setFocused(d)
              }}
              className={cn(
                'mx-auto box-border inline-flex size-7 cursor-pointer items-center justify-center rounded-sm tabular-nums',
                'hover:bg-(--colorNeutralBackground1Hover) active:bg-(--colorNeutralBackground1Pressed) focus-visible:outline-2 focus-visible:outline-(--colorStrokeFocus2)',
                inMonth ? 'text-fg1' : 'text-fg3',
                selected && 'bg-(--colorBrandBackground2) font-semibold text-(--colorBrandForeground2) hover:bg-(--colorBrandBackground2Hover)',
                // Fluent marks today with a brand-filled circle.
                isToday && 'rounded-full bg-(--colorBrandBackground) font-semibold text-(--colorNeutralForegroundOnBrand) hover:bg-(--colorBrandBackgroundHover)',
                disabled && 'cursor-not-allowed text-(--colorNeutralForegroundDisabled) hover:bg-transparent',
              )}
            >
              {Number(d.slice(8))}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function weekday(day: string): number {
  return new Date(`${day}T00:00:00Z`).getUTCDay()
}

/** Same day of month `n` months away, clamped to that month's last day. */
function addMonths(day: string, n: number): string {
  const d = new Date(`${day.slice(0, 7)}-01T00:00:00Z`)
  d.setUTCMonth(d.getUTCMonth() + n)
  const lastDay = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).getUTCDate()
  return `${d.toISOString().slice(0, 8)}${String(Math.min(Number(day.slice(8)), lastDay)).padStart(2, '0')}`
}

export { DatePicker }
