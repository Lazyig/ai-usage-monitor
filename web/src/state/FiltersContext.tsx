import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { getPreferences, putPreferences } from '../api/client'
import type { DashboardPreferences } from '../api/types'
import { type DateRange, type RangePreset, customRange, rangeForPreset } from '../lib/dateRange'
import { fromPreferences, toPreferences } from '../lib/preferences'
import { readStored, writeStored } from '../lib/storage'

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

/** This browser's copy of the server's document, so a reload starts with the
 * last selection instead of flashing the default until the server answers. */
const CACHE_KEY = 'aum_dashboard_prefs'
/** Selections are saved this long after the last change, so clicking through
 * presets sends one request. */
const SAVE_DELAY_MS = 400

function cached(): DashboardPreferences | null {
  try {
    return JSON.parse(readStored(CACHE_KEY) ?? 'null')
  } catch {
    return null
  }
}

export function FiltersProvider({ children }: { children: ReactNode }) {
  const [initial] = useState(() => fromPreferences(cached()))
  const [range, setRange] = useState<DateRange>(initial.range)
  const [autoRefresh, setAutoRefresh] = useState(initial.autoRefresh)
  const [refreshKey, setRefreshKey] = useState(0)
  // Set by the user's own changes: from then on they are saved, and a late
  // answer from the server no longer overrides them.
  const touched = useRef(false)

  // The server's copy wins over this browser's cache: it is what the operator
  // last chose on any browser.
  useEffect(() => {
    let cancelled = false
    getPreferences()
      .then((p) => {
        if (cancelled || touched.current) return
        const s = fromPreferences(p)
        setRange((current) => (current.preset === s.range.preset && current.from === s.range.from && current.to === s.range.to ? current : s.range))
        setAutoRefresh(s.autoRefresh)
      })
      .catch(() => {
        // Offline or an older server: keep the cached or default selection.
      })
    return () => {
      cancelled = true
    }
  }, [])

  const stored = JSON.stringify(toPreferences({ range, autoRefresh }))
  useEffect(() => {
    writeStored(CACHE_KEY, stored)
    if (!touched.current) return
    const id = setTimeout(() => {
      putPreferences(JSON.parse(stored)).catch(() => {
        // Not saved on the server; this browser still remembers it.
      })
    }, SAVE_DELAY_MS)
    return () => clearTimeout(id)
  }, [stored])

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
      setPreset: (preset) => {
        touched.current = true
        setRange(rangeForPreset(preset))
      },
      setCustomRange: (fromDate, toDateInclusive) => {
        touched.current = true
        setRange(customRange(fromDate, toDateInclusive))
      },
      autoRefresh,
      toggleAutoRefresh: () => {
        touched.current = true
        setAutoRefresh((v) => !v)
      },
      refreshKey,
      refresh,
    }),
    [range, autoRefresh, refreshKey, refresh],
  )

  return <FiltersContext.Provider value={value}>{children}</FiltersContext.Provider>
}

/** The refresh counter, or undefined outside a FiltersProvider. */
export function useRefreshKey(): number | undefined {
  return useContext(FiltersContext)?.refreshKey
}

export function useFilters(): FiltersContextValue {
  const ctx = useContext(FiltersContext)
  if (!ctx) throw new Error('useFilters must be used within FiltersProvider')
  return ctx
}
