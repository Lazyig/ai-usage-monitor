import { useEffect, useRef, useState } from 'react'
import { getEvents, type FilterParams, type RangeParams } from '../api/client'
import type { UsageEventDTO } from '../api/types'
import { describeError } from '../lib/errors'

type EventQuery = RangeParams & FilterParams & { model?: string }
const PAGE_SIZE = 100

/** First page wins overlapping event ids, preserving the server's order. */
function mergePages(first: UsageEventDTO[], second: UsageEventDTO[]): UsageEventDTO[] {
  const seen = new Set<string>()
  return [...first, ...second].filter((event) => {
    if (seen.has(event.event_id)) return false
    seen.add(event.event_id)
    return true
  })
}

/** Owns pagination and background refresh; rendering stays in the Events page. */
export function useEventFeed(query: EventQuery, refreshKey: number) {
  const [events, setEvents] = useState<UsageEventDTO[]>([])
  const [nextCursor, setNextCursor] = useState<string>()
  const [status, setStatus] = useState<'loading' | 'error' | 'success'>('loading')
  const [error, setError] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState<string | null>(null)
  const queryKey = JSON.stringify(query)
  const currentQuery = useRef(queryKey)
  currentQuery.current = queryKey
  const shownQuery = useRef<string | null>(null)
  const pagesLoaded = useRef(0)
  const pendingPage = useRef<symbol | null>(null)

  useEffect(() => {
    let cancelled = false
    const isRefresh = shownQuery.current === queryKey
    if (!isRefresh) {
      shownQuery.current = null
      setStatus('loading')
      setEvents([])
      setNextCursor(undefined)
      setError(null)
      setLoadMoreError(null)
      setLoadingMore(false)
      pendingPage.current = null
      pagesLoaded.current = 0
    }
    getEvents({ ...query, limit: PAGE_SIZE })
      .then((r) => {
        if (cancelled) return
        if (isRefresh && pagesLoaded.current > 1) {
          setEvents((prev) => mergePages(r.events, prev))
        } else {
          setEvents(r.events)
          setNextCursor(r.next_cursor)
          pagesLoaded.current = 1
        }
        shownQuery.current = queryKey
        setStatus('success')
      })
      .catch((err: unknown) => {
        if (cancelled || isRefresh) return
        setError(describeError(err))
        setStatus('error')
      })
    return () => { cancelled = true }
    // queryKey represents the complete query, including optional filters.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [queryKey, refreshKey])

  useEffect(() => () => { pendingPage.current = null }, [])

  async function loadMore() {
    if (!nextCursor || pendingPage.current) return
    const request = Symbol()
    pendingPage.current = request
    setLoadingMore(true)
    setLoadMoreError(null)
    const isCurrent = () => currentQuery.current === queryKey && pendingPage.current === request
    try {
      const r = await getEvents({ ...query, limit: PAGE_SIZE, cursor: nextCursor })
      if (!isCurrent()) return
      setEvents((prev) => mergePages(prev, r.events))
      setNextCursor(r.next_cursor)
      pagesLoaded.current += 1
    } catch (err) {
      if (isCurrent()) setLoadMoreError(describeError(err))
    } finally {
      if (pendingPage.current === request) {
        pendingPage.current = null
        setLoadingMore(false)
      }
    }
  }

  return { events, nextCursor, status, error, loadingMore, loadMoreError, loadMore }
}
