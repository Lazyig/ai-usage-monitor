import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { getPreferences, getStoredToken, putPreferences } from '../api/client'
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
 * last selection instead of flashing the default until the server answers.
 * `synced: false` marks a change the server has not confirmed yet: it wins
 * over the server's copy and is sent again on the next load. */
const CACHE_KEY = 'aum_dashboard_prefs'
/** Selections are saved this long after the last change, so clicking through
 * presets sends one request. */
const SAVE_DELAY_MS = 400

interface Cached {
  prefs: DashboardPreferences
  synced: boolean
}

function parseCache(raw: string | null): Cached | null {
  try {
    const v = JSON.parse(raw ?? 'null')
    return v && typeof v === 'object' && 'prefs' in v ? { prefs: v.prefs, synced: v.synced !== false } : null
  } catch {
    return null
  }
}

const docOf = (c: Cached | null) => (c ? JSON.stringify(c.prefs) : '')
const writeCache = (doc: string, synced: boolean) => writeStored(CACHE_KEY, JSON.stringify({ prefs: JSON.parse(doc), synced }))

export function FiltersProvider({ children }: { children: ReactNode }) {
  const [initial] = useState(() => fromPreferences(parseCache(readStored(CACHE_KEY))?.prefs))
  const [range, setRange] = useState<DateRange>(initial.range)
  const [autoRefresh, setAutoRefresh] = useState(initial.autoRefresh)
  const [refreshKey, setRefreshKey] = useState(0)
  // Set by the user's own changes: a late answer from the server no longer
  // overrides them.
  const touched = useRef(false)
  // Set once another tab's change was followed: the server's answer to this
  // tab's own load is older than that.
  const followed = useRef(false)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const latest = useRef('')
  // The token a pending save goes out with: logout clears the stored one
  // before this provider unmounts and flushes.
  const latestToken = useRef<string | null>(null)

  const apply = useCallback((prefs: DashboardPreferences | undefined) => {
    const s = fromPreferences(prefs)
    setRange((current) => (current.preset === s.range.preset && current.from === s.range.from && current.to === s.range.to ? current : s.range))
    setAutoRefresh(s.autoRefresh)
  }, [])

  /** Sends doc to the server; the cache is marked synced once it lands. */
  const save = useCallback((doc: string, keepalive = false, token = getStoredToken()) => {
    if (!token) return // no session: the unsynced cache is sent after the next login
    putPreferences(JSON.parse(doc), { keepalive, token })
      .then(() => {
        if (docOf(parseCache(readStored(CACHE_KEY))) === doc) writeCache(doc, true)
      })
      .catch(() => {
        // Stays unsynced in this browser and is sent again on the next load.
      })
  }, [])

  /** Sends a save still waiting for its delay right away (page hidden, closed, logout). */
  const flush = useCallback(() => {
    if (!timer.current) return
    clearTimeout(timer.current)
    timer.current = null
    save(latest.current, true, latestToken.current)
  }, [save])

  useEffect(() => {
    const cache = parseCache(readStored(CACHE_KEY))
    if (cache && !cache.synced) {
      // A change the server never got (a reload within the save delay, a
      // failed request): it is newer than the server's copy.
      touched.current = true
      save(docOf(cache))
      return
    }
    let cancelled = false
    getPreferences()
      .then((p) => {
        if (!cancelled && !touched.current && !followed.current) apply(p)
      })
      .catch(() => {
        // Offline or an older server: keep the cached or default selection.
      })
    return () => {
      cancelled = true
    }
  }, [apply, save])

  // Another tab changed the selection: follow it (that tab saves it).
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key !== CACHE_KEY) return
      followed.current = true
      // The other tab's change is newer than one this tab still meant to save.
      if (timer.current) {
        clearTimeout(timer.current)
        timer.current = null
      }
      apply(parseCache(e.newValue)?.prefs)
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [apply])

  const stored = JSON.stringify(toPreferences({ range, autoRefresh }))
  useEffect(() => {
    const cache = parseCache(readStored(CACHE_KEY))
    // Equal to the cache: loaded from it, or another tab's change.
    if (stored === docOf(cache)) return
    if (!touched.current) {
      // The server's copy (or the default) just applied. A change another tab
      // has not saved yet is newer: leave it.
      if (!cache || cache.synced) writeCache(stored, true)
      return
    }
    writeCache(stored, false)
    latest.current = stored
    latestToken.current = getStoredToken()
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      timer.current = null
      save(stored)
    }, SAVE_DELAY_MS)
  }, [stored, save])

  // A pending save goes out before the page is hidden, closed or this
  // provider unmounts (logout, navigating away).
  useEffect(() => {
    const onHide = () => {
      if (document.visibilityState === 'hidden') flush()
    }
    window.addEventListener('pagehide', flush)
    document.addEventListener('visibilitychange', onHide)
    return () => {
      window.removeEventListener('pagehide', flush)
      document.removeEventListener('visibilitychange', onHide)
      flush()
    }
  }, [flush])

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
