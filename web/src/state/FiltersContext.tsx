import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
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

  useEffect(() => {
    if (!autoRefresh) return
    const id = setInterval(() => setRefreshKey((k) => k + 1), AUTO_REFRESH_MS)
    return () => clearInterval(id)
  }, [autoRefresh])

  const value = useMemo<FiltersContextValue>(
    () => ({
      range,
      setPreset: (preset) => setRange(rangeForPreset(preset)),
      setCustomRange: (fromDate, toDateInclusive) => setRange(customRange(fromDate, toDateInclusive)),
      autoRefresh,
      toggleAutoRefresh: () => setAutoRefresh((v) => !v),
      refreshKey,
      refresh: () => setRefreshKey((k) => k + 1),
    }),
    [range, autoRefresh, refreshKey],
  )

  return <FiltersContext.Provider value={value}>{children}</FiltersContext.Provider>
}

export function useFilters(): FiltersContextValue {
  const ctx = useContext(FiltersContext)
  if (!ctx) throw new Error('useFilters must be used within FiltersProvider')
  return ctx
}
