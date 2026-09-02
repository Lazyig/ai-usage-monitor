// Date range presets, computed against Asia/Seoul calendar days (matching the
// API's default tz). `to` is exclusive, per docs/API.md.

export type RangePreset = 'today' | '7d' | '30d' | 'thisMonth' | 'lastMonth' | 'all' | 'custom'

/** Earliest supported `from` for the "전체" (all-time) preset. The API
 * defaults `from` to "last 30 days" when the param is omitted, so this
 * preset must send an explicit, sufficiently-early date instead of leaving
 * `from` blank. */
const ALL_TIME_FROM = '2020-01-01'

export interface DateRange {
  preset: RangePreset
  /** YYYY-MM-DD, inclusive */
  from: string
  /** YYYY-MM-DD, exclusive (already +1 day past the last included day) */
  to: string
}

export const TIMEZONE = 'Asia/Seoul'

export function kstToday(): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: TIMEZONE }).format(new Date())
}

export function addDays(dateStr: string, days: number): string {
  const d = new Date(`${dateStr}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + days)
  return d.toISOString().slice(0, 10)
}

function firstOfMonth(dateStr: string): string {
  return `${dateStr.slice(0, 7)}-01`
}

export function rangeForPreset(preset: RangePreset, today = kstToday()): DateRange {
  switch (preset) {
    case 'today':
      return { preset, from: today, to: addDays(today, 1) }
    case '7d':
      return { preset, from: addDays(today, -6), to: addDays(today, 1) }
    case '30d':
      return { preset, from: addDays(today, -29), to: addDays(today, 1) }
    case 'thisMonth':
      return { preset, from: firstOfMonth(today), to: addDays(today, 1) }
    case 'lastMonth': {
      const thisMonthFirst = firstOfMonth(today)
      const lastMonthLastDay = addDays(thisMonthFirst, -1)
      return { preset, from: firstOfMonth(lastMonthLastDay), to: thisMonthFirst }
    }
    case 'all':
      return { preset, from: ALL_TIME_FROM, to: addDays(today, 1) }
    case 'custom':
      return { preset, from: today, to: addDays(today, 1) }
  }
}

/** Whole days covered by [from, to) — used to decide whether an hourly
 * bucket would still be readable. */
export function rangeDays(from: string, to: string): number {
  const ms = Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)
  if (Number.isNaN(ms)) return 1
  return Math.max(1, Math.round(ms / 86_400_000))
}

/** Longest range that still makes sense as hourly bars. An hourly bucket over
 * a month is ~720 bars — unreadable, and a large payload. */
export const HOURLY_MAX_DAYS = 7

/** Whether the hourly bucket may be offered for this range. A range change
 * that outgrows it silently falls the caller back to daily. */
export function hourlyAllowed(range: DateRange): boolean {
  return rangeDays(range.from, range.to) <= HOURLY_MAX_DAYS
}

export const PRESET_LABELS: Record<RangePreset, string> = {
  today: '오늘',
  '7d': '최근 7일',
  '30d': '최근 30일',
  thisMonth: '이번 달',
  lastMonth: '지난 달',
  all: '전체',
  custom: '직접 선택',
}

/** Builds a custom range from inclusive from/to date inputs (end date inclusive as entered by the user). */
export function customRange(fromDate: string, toDateInclusive: string): DateRange {
  return { preset: 'custom', from: fromDate, to: addDays(toDateInclusive, 1) }
}
