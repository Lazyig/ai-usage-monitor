import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { type DateRange, type RangePreset, customRange, rangeForPreset } from '../lib/dateRange'

interface FiltersContextValue {
  range: DateRange
  setPreset: (preset: RangePreset) => void
  setCustomRange: (fromDate: string, toDateInclusive: string) => void
  autoRefresh: boolean
  toggleAutoRefresh: () => void
  /** Bump to force every page's data hooks to refetch. */
  refreshKey: number
  refresh: () => void
}

const FiltersContext = createContext<FiltersContextValue | null>(null)

const AUTO_REFRESH_MS = 60_000

export function FiltersProvider({ children }: { children: ReactNode }) {
  const [range, setRange] = useState<DateRange>(() => rangeForPreset('30d'))
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)

  const syncRange = useCallback(() => {
    setRange((current) => {
      if (current.preset === 'custom') return current
      const next = rangeForPreset(current.preset)
      return current.from === next.from && current.to === next.to ? current : next
    })
  }, [])

  const refresh = useCallback(() => {
    syncRange()
    setRefreshKey((k) => k + 1)
  }, [syncRange])

  // Keep relative presets current even when automatic data refresh is off.
  // Focus/visibility events also cover sleeping devices and throttled tabs.
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === 'visible') syncRange()
    }
    const id = setInterval(syncRange, AUTO_REFRESH_MS)
    window.addEventListener('focus', syncRange)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(id)
      window.removeEventListener('focus', syncRange)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [syncRange])

  useEffect(() => {
    if (!autoRefresh) return
    // A hidden tab skips its refreshes; coming back to it refreshes at once.
    const tick = () => {
      if (document.visibilityState !== 'hidden') refresh()
    }
    const onVisible = () => {
      if (document.visibilityState === 'visible') refresh()
    }
    const id = setInterval(tick, AUTO_REFRESH_MS)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(id)
      document.removeEventListener('visibilitychange', onVisible)
    }
  }, [autoRefresh, refresh])

  const value = useMemo<FiltersContextValue>(
    () => ({
      range,
      setPreset: (preset) => setRange(rangeForPreset(preset)),
      setCustomRange: (fromDate, toDateInclusive) => setRange(customRange(fromDate, toDateInclusive)),
      autoRefresh,
      toggleAutoRefresh: () => setAutoRefresh((v) => !v),
      refreshKey,
      refresh,
    }),
    [range, autoRefresh, refreshKey, refresh],
  )

  return <FiltersContext.Provider value={value}>{children}</FiltersContext.Provider>
}

export function useFilters(): FiltersContextValue {
  const ctx = useContext(FiltersContext)
  if (!ctx) throw new Error('useFilters must be used within FiltersProvider')
  return ctx
}
