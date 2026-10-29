// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { DashboardPreferences } from '../api/types'
import { FiltersProvider, useFilters } from './FiltersContext'

const api = vi.hoisted(() => ({
  serverPrefs: {} as DashboardPreferences,
  saved: [] as DashboardPreferences[],
  fail: false,
}))

vi.mock('../api/client', () => ({
  getStoredToken: () => 'token',
  getPreferences: () => Promise.resolve(api.serverPrefs),
  putPreferences: (p: DashboardPreferences) => {
    api.saved.push(p)
    return api.fail ? Promise.reject(new Error('offline')) : Promise.resolve(p)
  },
}))

let filters: ReturnType<typeof useFilters>
let root: Root
let container: HTMLDivElement

function Probe() {
  filters = useFilters()
  return null
}

beforeEach(() => {
  localStorage.clear()
  api.serverPrefs = {}
  api.saved = []
  api.fail = false
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
  it.each(['day-0', 'week-0', 'last-7', 'last-30', 'month-0', 'all'] as const)(
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
    act(() => filters.setPreset('day-0'))
    act(() => vi.advanceTimersByTime(60_000))
    expect(filters.range).toEqual({ preset: 'day-0', from: '2026-10-16', to: '2026-10-17' })
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
    act(() => filters.setPreset('month-1'))
    expect(filters.range).toEqual({ preset: 'month-1', from: '2026-11-01', to: '2026-12-01' })
    vi.setSystemTime(new Date('2026-12-31T15:00:01Z'))
    act(() => filters.refresh())
    expect(filters.range).toEqual({ preset: 'month-1', from: '2026-12-01', to: '2027-01-01' })
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
    act(() => vi.advanceTimersByTime(400)) // the save
    expect(vi.getTimerCount()).toBe(2)
    act(() => root.unmount())
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('selections kept on the server', () => {
  async function remount() {
    act(() => root.unmount())
    root = createRoot(container)
    await act(async () => root.render(<FiltersProvider><Probe /></FiltersProvider>))
  }

  it('saves what the user picks, once the picking settles', () => {
    act(() => filters.setPreset('day-1'))
    act(() => filters.setPreset('last-7'))
    act(() => filters.toggleAutoRefresh())
    expect(api.saved).toEqual([])
    act(() => vi.advanceTimersByTime(400))
    expect(api.saved).toEqual([{ range: { preset: 'last-7' }, auto_refresh: true }])
  })

  it('keeps the dates of a custom range only', () => {
    act(() => filters.setCustomRange('2026-10-01', '2026-10-10'))
    act(() => vi.advanceTimersByTime(400))
    expect(api.saved).toEqual([{ range: { preset: 'custom', from: '2026-10-01', to: '2026-10-11' }, auto_refresh: false }])
  })

  it('starts from the server copy, with relative presets recomputed for today', async () => {
    api.serverPrefs = { range: { preset: 'day-1' }, auto_refresh: true }
    await remount()
    expect(filters.range).toEqual({ preset: 'day-1', from: '2026-10-14', to: '2026-10-15' })
    expect(filters.autoRefresh).toBe(true)
    // Loading is not a change: nothing is written back.
    act(() => vi.advanceTimersByTime(400))
    expect(api.saved).toEqual([])
  })

  it('starts from this browser\'s copy before the server answers, and ignores bad documents', async () => {
    localStorage.setItem('aum_dashboard_prefs', JSON.stringify({ prefs: { range: { preset: 'month-0' }, auto_refresh: true }, synced: true }))
    api.serverPrefs = { range: { preset: 'nonsense' }, auto_refresh: 'yes' as unknown as boolean }
    act(() => root.unmount())
    root = createRoot(container)
    act(() => root.render(<FiltersProvider><Probe /></FiltersProvider>))
    expect(filters.range.preset).toBe('month-0')
    expect(filters.autoRefresh).toBe(true)
    // The server's (unusable) document then means the defaults.
    await act(async () => {})
    expect(filters.range.preset).toBe('last-30')
    expect(filters.autoRefresh).toBe(false)
  })

  it('does not let a late server answer undo a change the user already made', async () => {
    api.serverPrefs = { range: { preset: 'all' } }
    act(() => root.unmount())
    root = createRoot(container)
    act(() => root.render(<FiltersProvider><Probe /></FiltersProvider>))
    act(() => filters.setPreset('day-0'))
    await act(async () => {})
    expect(filters.range.preset).toBe('day-0')
  })

  it('sends a change made just before a reload on the next load, ahead of the server copy', async () => {
    api.serverPrefs = { range: { preset: 'last-7' } }
    // Reloaded within the save delay, and the last-moment send is lost.
    api.fail = true
    act(() => filters.setPreset('all'))
    act(() => root.unmount())
    await act(async () => {})
    api.fail = false
    api.saved = []
    root = createRoot(container)
    await act(async () => root.render(<FiltersProvider><Probe /></FiltersProvider>))
    expect(filters.range.preset).toBe('all')
    expect(api.saved).toEqual([{ range: { preset: 'all' }, auto_refresh: false }])
    await act(async () => {})
    expect(JSON.parse(localStorage.getItem('aum_dashboard_prefs')!).synced).toBe(true)
  })

  it('keeps a change the server refused to take as unsynced', async () => {
    api.fail = true
    act(() => filters.setPreset('week-1'))
    await act(async () => vi.advanceTimersByTime(400))
    expect(JSON.parse(localStorage.getItem('aum_dashboard_prefs')!)).toEqual({ prefs: { range: { preset: 'week-1' }, auto_refresh: false }, synced: false })
  })

  it('sends a pending save at once when the page is hidden', () => {
    act(() => filters.setPreset('month-2'))
    act(() => window.dispatchEvent(new Event('pagehide')))
    expect(api.saved).toEqual([{ range: { preset: 'month-2' }, auto_refresh: false }])
    act(() => vi.advanceTimersByTime(400))
    expect(api.saved).toHaveLength(1)
  })

  it('follows a change made in another tab without saving it again', () => {
    const doc = { prefs: { range: { preset: 'day-2' }, auto_refresh: true }, synced: true }
    localStorage.setItem('aum_dashboard_prefs', JSON.stringify(doc))
    act(() => window.dispatchEvent(new StorageEvent('storage', { key: 'aum_dashboard_prefs', newValue: JSON.stringify(doc) })))
    expect(filters.range.preset).toBe('day-2')
    expect(filters.autoRefresh).toBe(true)
    act(() => vi.advanceTimersByTime(400))
    expect(api.saved).toEqual([])
  })
})
