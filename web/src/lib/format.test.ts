import { describe, expect, it } from 'vitest'
import { formatCompact, formatCompactIn } from './format'

describe('formatCompactIn', () => {
  it.each([
    [0, '0', '0'],
    [12.75, '12.8', '12.8'],
    [999, '999', '999'],
    [8_420, '8.4K', '8,420'],
    [9_999.96, '10K', '1만'],
    [10_000, '10K', '1만'],
    [356_000, '356K', '35.6만'],
    [12_345_678, '12.3M', '1,234.6만'],
    [47_800_000, '47.8M', '4,780만'],
    [100_000_000, '100M', '1억'],
    [2_150_000_000, '2.2B', '21.5억'],
    [1_200_000_000_000, '1.2T', '1.2조'],
    [-356_000, '-356K', '-35.6만'],
  ])('%d → %s (en) / %s (ko)', (n, en, ko) => {
    expect(formatCompactIn(n, 'en')).toBe(en)
    expect(formatCompactIn(n, 'ko')).toBe(ko)
  })
})

describe('formatCompact', () => {
  it('ignores a second argument, as a chart axis passes the tick index', () => {
    const tick = formatCompact as (v: number, index: number) => string
    expect(tick(356_000, 0)).toBe('356K')
  })
})
