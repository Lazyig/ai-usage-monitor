import { describe, expect, it } from 'vitest'
import { rangeForPreset } from '../lib/dateRange'
import { PRESETS, visiblePresets } from './PeriodTabs'

const widths = Object.fromEntries(PRESETS.map((p) => [p, 60])) as Record<(typeof PRESETS)[number], number>
// 8 tabs x 60 + 7 gaps x 6 = 522; the menu button is 32 (+6 gap).

describe('visiblePresets', () => {
  it('shows every preset when they fit', () => {
    expect(visiblePresets(522, widths, 32, '30d')).toEqual(PRESETS)
  })

  it('moves the least used presets into the menu first, keeping display order', () => {
    // Room for 5 tabs and the menu: 5*60 + 4*6 + 6 + 32 = 362.
    expect(visiblePresets(362, widths, 32, '30d')).toEqual(['today', 'yesterday', '7d', '30d', 'thisMonth'])
  })

  it('keeps the selected preset and 오늘 visible however narrow', () => {
    expect(visiblePresets(10, widths, 32, 'all')).toEqual(['today', 'all'])
  })

  it('reserves room for the menu when it holds other controls', () => {
    expect(visiblePresets(522, widths, 32, '30d', true)).toEqual(PRESETS.filter((p) => p !== 'all'))
  })
})

describe('yesterday', () => {
  it('is the previous Seoul calendar day, end exclusive', () => {
    expect(rangeForPreset('yesterday', '2026-11-01')).toEqual({ preset: 'yesterday', from: '2026-10-31', to: '2026-11-01' })
  })
})
