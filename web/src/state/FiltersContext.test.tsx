// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FiltersProvider, useFilters } from './FiltersContext'

let filters: ReturnType<typeof useFilters>
let root: Root
let container: HTMLDivElement

function Probe() {
  filters = useFilters()
  return null
}

beforeEach(() => {
  vi.useFakeTimers()
  // Immediately before midnight in Seoul (UTC+9).
  vi.setSystemTime(new Date('2026-10-15T14:59:30Z'))
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  act(() => root.render(<FiltersProvider><Probe /></FiltersProvider>))
})

afterEach(() => {
  act(() => root.unmount())
  container.remove()
  vi.useRealTimers()
})

describe('date range refresh across Seoul midnight', () => {
  it.each(['today', '7d', '30d', 'thisMonth', 'all'] as const)(
    'recalculates %s on manual refresh and refetches data',
    (preset) => {
      act(() => filters.setPreset(preset))
      expect(filters.range.to).toBe('2026-10-16')
      vi.setSystemTime(new Date('2026-10-15T15:00:01Z'))
      act(() => filters.refresh())
      expect(filters.range.to).toBe('2026-10-17')
      expect(filters.refreshKey).toBe(1)
    },
  )

  it('advances today while the page stays open with auto-refresh disabled', () => {
    act(() => filters.setPreset('today'))
    act(() => vi.advanceTimersByTime(60_000))
    expect(filters.range).toEqual({ preset: 'today', from: '2026-10-16', to: '2026-10-17' })
    expect(filters.refreshKey).toBe(0)
  })

  it('refreshes the range and data together with auto-refresh enabled', () => {
    act(() => filters.toggleAutoRefresh())
    act(() => vi.advanceTimersByTime(60_000))
    expect(filters.range.to).toBe('2026-10-17')
    expect(filters.refreshKey).toBe(1)
  })

  it.each(['focus', 'visibilitychange'])('updates after returning to a sleeping tab via %s', (event) => {
    vi.setSystemTime(new Date('2026-10-17T01:00:00Z'))
    act(() => {
      if (event === 'focus') window.dispatchEvent(new Event(event))
      else document.dispatchEvent(new Event(event))
    })
    expect(filters.range.to).toBe('2026-10-18')
  })

  it('recalculates last month across a month and year boundary', () => {
    vi.setSystemTime(new Date('2026-12-31T14:59:30Z'))
    act(() => filters.setPreset('lastMonth'))
    expect(filters.range).toEqual({ preset: 'lastMonth', from: '2026-11-01', to: '2026-12-01' })
    vi.setSystemTime(new Date('2026-12-31T15:00:01Z'))
    act(() => filters.refresh())
    expect(filters.range).toEqual({ preset: 'lastMonth', from: '2026-12-01', to: '2027-01-01' })
  })

  it('preserves custom dates during manual, timed, and focus refreshes', () => {
    act(() => filters.setCustomRange('2026-10-01', '2026-10-10'))
    act(() => {
      vi.advanceTimersByTime(60_000)
      filters.refresh()
      window.dispatchEvent(new Event('focus'))
    })
    expect(filters.range).toEqual({ preset: 'custom', from: '2026-10-01', to: '2026-10-11' })
    expect(filters.refreshKey).toBe(1)
  })

  it('removes timers when unmounted', () => {
    act(() => filters.toggleAutoRefresh())
    expect(vi.getTimerCount()).toBe(2)
    act(() => root.unmount())
    expect(vi.getTimerCount()).toBe(0)
  })
})
