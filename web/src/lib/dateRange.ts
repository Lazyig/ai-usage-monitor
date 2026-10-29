// Date range presets, computed against Asia/Seoul calendar days (matching the
// API's default tz). `to` is exclusive, per docs/API.md.
//
// A preset names a calendar period counted back from today ("day-0" is today,
// "day-1" yesterday, "week-1" last week, "month-2" the month before last), a
// rolling window ending today ("last-30"), everything, or custom dates.

export type PeriodUnit = 'day' | 'week' | 'month'
export type RangePreset = `${PeriodUnit}-${number}` | `last-${number}` | 'all' | 'custom'

/** How far back each unit's menu goes, and the rolling windows offered. */
export const BACK: Record<PeriodUnit, number> = { day: 6, week: 4, month: 12 }
export const LAST_DAYS = [7, 30, 90] as const

export const DEFAULT_PRESET: RangePreset = 'last-30'

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

/** The first of the month `months` after dateStr's month (negative: before). */
function addMonths(dateStr: string, months: number): string {
  const d = new Date(`${dateStr.slice(0, 7)}-01T00:00:00Z`)
  d.setUTCMonth(d.getUTCMonth() + months)
  return d.toISOString().slice(0, 10)
}

/** Monday of dateStr's week (weeks start on Monday). */
function mondayOf(dateStr: string): string {
  const dow = new Date(`${dateStr}T00:00:00Z`).getUTCDay() // 0 = Sunday
  return addDays(dateStr, -((dow + 6) % 7))
}

/** A preset split into its parts, or null when it is not one this app offers. */
export function parsePreset(v: unknown): { kind: PeriodUnit | 'last'; n: number } | { kind: 'all' | 'custom' } | null {
  if (v === 'all' || v === 'custom') return { kind: v }
  if (typeof v !== 'string') return null
  const m = /^(day|week|month|last)-(\d{1,2})$/.exec(v)
  if (!m) return null
  const kind = m[1] as PeriodUnit | 'last'
  const n = Number(m[2])
  if (kind === 'last') return (LAST_DAYS as readonly number[]).includes(n) ? { kind, n } : null
  return n <= BACK[kind] ? { kind, n } : null
}

export function isPreset(v: unknown): v is RangePreset {
  return parsePreset(v) !== null
}

export function rangeForPreset(preset: RangePreset, today = kstToday()): DateRange {
  const p = parsePreset(preset)
  const tomorrow = addDays(today, 1)
  switch (p?.kind) {
    case 'day':
      return { preset, from: addDays(today, -p.n), to: addDays(today, 1 - p.n) }
    case 'week': {
      const from = addDays(mondayOf(today), -7 * p.n)
      return { preset, from, to: p.n === 0 ? tomorrow : addDays(from, 7) }
    }
    case 'month':
      return { preset, from: addMonths(today, -p.n), to: p.n === 0 ? tomorrow : addMonths(today, 1 - p.n) }
    case 'last':
      return { preset, from: addDays(today, 1 - p.n), to: tomorrow }
    case 'all':
      return { preset, from: ALL_TIME_FROM, to: tomorrow }
    case 'custom':
      return { preset, from: today, to: tomorrow }
  }
  return rangeForPreset(DEFAULT_PRESET, today)
}

/** The preset's name as shown on its tab: 오늘, 어제, 3일 전, 지난주, 8월, 최근 7일 … */
export function presetLabel(preset: RangePreset, today = kstToday()): string {
  const p = parsePreset(preset)
  switch (p?.kind) {
    case 'day':
      return p.n === 0 ? '오늘' : p.n === 1 ? '어제' : `${p.n}일 전`
    case 'week':
      return p.n === 0 ? '이번 주' : p.n === 1 ? '지난주' : `${p.n}주 전`
    case 'month': {
      if (p.n === 0) return '이번 달'
      if (p.n === 1) return '지난달'
      const first = addMonths(today, -p.n)
      const month = `${Number(first.slice(5, 7))}월`
      return first.slice(0, 4) === today.slice(0, 4) ? month : `${first.slice(0, 4)}년 ${month}`
    }
    case 'last':
      return `최근 ${p.n}일`
    case 'all':
      return '전체'
    case 'custom':
      return '직접 선택'
  }
  return ''
}

/** What a menu entry adds after the label: the dates of a past week, the
 * month of 지난달. */
export function presetDetail(preset: RangePreset, today = kstToday()): string {
  const p = parsePreset(preset)
  if (p?.kind === 'week' && p.n > 0) {
    const r = rangeForPreset(preset, today)
    const md = (d: string) => `${Number(d.slice(5, 7))}/${Number(d.slice(8, 10))}`
    return `${md(r.from)}–${md(addDays(r.to, -1))}`
  }
  if (p?.kind === 'month' && p.n === 1) return `${Number(addMonths(today, -1).slice(5, 7))}월`
  return ''
}

/** Whole days covered by [from, to) — used to decide whether an hourly
 * bucket would still be readable. */
function rangeDays(from: string, to: string): number {
  const ms = Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)
  if (Number.isNaN(ms)) return 1
  return Math.max(1, Math.round(ms / 86_400_000))
}

/** Longest range that still makes sense as hourly bars. An hourly bucket over
 * a month is ~720 bars — unreadable, and a large payload. */
const HOURLY_MAX_DAYS = 7

/** Whether the 일별/시간별 toggle is shown for this range. Beyond the limit the
 * toggle is hidden and the caller silently falls back to daily. */
export function hourlyAllowed(range: DateRange): boolean {
  return rangeDays(range.from, range.to) <= HOURLY_MAX_DAYS
}

/** Builds a custom range from inclusive from/to date inputs (end date inclusive as entered by the user). */
export function customRange(fromDate: string, toDateInclusive: string): DateRange {
  return { preset: 'custom', from: fromDate, to: addDays(toDateInclusive, 1) }
}
