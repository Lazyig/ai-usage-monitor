import { describe, expect, it } from 'vitest'
import { isPreset, presetDetail, presetLabel, rangeForPreset } from '../lib/dateRange'
import { PERIOD_ITEMS, type PeriodItem, visibleItems } from './PeriodTabs'

type Key = PeriodItem['key']
const keys = PERIOD_ITEMS.map((i) => i.key)
const widths = Object.fromEntries(keys.map((k) => [k, 60])) as Record<Key, number>
// 6 items x 60 + 5 gaps x 6 = 390; the menu button is 32 (+6 gap).

describe('visibleItems', () => {
  it('shows every item when they fit', () => {
    expect(visibleItems(390, widths, 32, 'last-30')).toEqual(keys)
  })

  it('moves the least used items into the menu first, keeping display order', () => {
    // Room for 4 items and the menu: 4*60 + 3*6 + 6 + 32 = 296.
    expect(visibleItems(296, widths, 32, 'day-0')).toEqual(['day', 'week', 'month', 'custom'])
  })

  it('keeps 오늘, 이번 주 and the selected item however narrow', () => {
    expect(visibleItems(10, widths, 32, 'all')).toEqual(['day', 'week', 'all'])
    expect(visibleItems(10, widths, 32, 'month-3')).toEqual(['day', 'week', 'month'])
  })

  it('gives up the extras first, the last one first', () => {
    const w = { ...widths, auto: 100, tz: 50 }
    // Everything: 390 + 6 + 100 + 6 + 50 = 552.
    expect(visibleItems(552, w, 32, 'last-30', ['auto', 'tz'])).toEqual([...keys, 'auto', 'tz'])
    // Without tz, with the menu: 390 + 6 + 100 + 6 + 32 = 534.
    expect(visibleItems(534, w, 32, 'last-30', ['auto', 'tz'])).toEqual([...keys, 'auto'])
    expect(visibleItems(433, w, 32, 'last-30', ['auto', 'tz'])).toEqual(keys)
  })
})

describe('calendar presets (Seoul days, end exclusive)', () => {
  // 2026-10-29 is a Thursday.
  const today = '2026-10-29'
  it.each([
    ['day-0', '2026-10-29', '2026-10-30'],
    ['day-1', '2026-10-28', '2026-10-29'],
    ['day-6', '2026-10-23', '2026-10-24'],
    ['week-0', '2026-10-26', '2026-10-30'],
    ['week-1', '2026-10-19', '2026-10-26'],
    ['week-4', '2026-09-28', '2026-10-05'],
    ['month-0', '2026-10-01', '2026-10-30'],
    ['month-1', '2026-09-01', '2026-10-01'],
    ['month-12', '2025-10-01', '2025-11-01'],
    ['last-7', '2026-10-23', '2026-10-30'],
    ['last-90', '2026-08-01', '2026-10-30'],
  ] as const)('%s', (preset, from, to) => {
    expect(rangeForPreset(preset, today)).toEqual({ preset, from, to })
  })

  it('starts the week on Monday, also on a Sunday', () => {
    expect(rangeForPreset('week-0', '2026-11-01')).toEqual({ preset: 'week-0', from: '2026-10-26', to: '2026-11-02' })
  })

  it('names the periods', () => {
    expect(['day-0', 'day-1', 'day-3', 'week-0', 'week-1', 'week-2', 'month-0', 'month-1', 'month-2', 'month-11', 'last-7', 'all', 'custom'].map((p) => presetLabel(p as never, today))).toEqual([
      '오늘', '어제', '3일 전', '이번 주', '지난주', '2주 전', '이번 달', '지난달', '8월', '2025년 11월', '최근 7일', '전체', '직접 선택',
    ])
    expect(presetDetail('week-1', today)).toBe('10/19–10/25')
    expect(presetDetail('month-1', today)).toBe('9월')
  })

  it('accepts only the offered presets', () => {
    for (const p of ['day-0', 'day-6', 'week-4', 'month-12', 'last-7', 'last-30', 'last-90', 'all', 'custom']) expect(isPreset(p)).toBe(true)
    for (const p of ['day-7', 'week-5', 'month-13', 'last-14', 'day--1', 'today', '30d', '', 'day-01x', 'day-01', 'month-00']) expect(isPreset(p)).toBe(false)
  })
})
