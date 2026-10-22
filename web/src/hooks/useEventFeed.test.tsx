// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { getEvents } from '../api/client'
import type { EventsResponse, UsageEventDTO } from '../api/types'
import { useEventFeed } from './useEventFeed'

vi.mock('../api/client', async (importOriginal) => ({
  ...await importOriginal<typeof import('../api/client')>(),
  getEvents: vi.fn(),
}))

const pending: { resolve: (r: EventsResponse) => void; reject: (e: Error) => void }[] = []
let root: Root
let container: HTMLDivElement
let feed: ReturnType<typeof useEventFeed>

function Probe({ model = '', refresh = 0 }: { model?: string; refresh?: number }) {
  feed = useEventFeed({ from: '2026-10-01', to: '2026-10-24', model }, refresh)
  return null
}

function event(id: string): UsageEventDTO {
  return { event_id: id, device_id: 'test', source: 'codex', ts: '2026-10-23T00:00:00Z', model: 'gpt-6-sol', input: 1, output: 1, cache_read: 0, cache_write_5m: 0, cache_write_1h: 0, cache_write: 0, reasoning: 0, confidence: 'exact', cost_usd: 0.01 }
}

async function resolve(index: number, ids: string[], cursor?: string) {
  await act(async () => pending[index].resolve({ events: ids.map(event), next_cursor: cursor }))
}

beforeEach(() => {
  pending.length = 0
  vi.mocked(getEvents).mockReset().mockImplementation(() => new Promise((resolve, reject) => pending.push({ resolve, reject })))
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  container = document.createElement('div')
  root = createRoot(container)
  act(() => root.render(<Probe />))
})

afterEach(() => act(() => root.unmount()))

it('ignores a late first page after the filters change', async () => {
  act(() => root.render(<Probe model="new" />))
  await resolve(1, ['new'])
  await resolve(0, ['old'])
  expect(feed.events.map((e) => e.event_id)).toEqual(['new'])
})

it('prevents concurrent pagination and removes overlaps', async () => {
  await resolve(0, ['a', 'b'], 'cursor')
  act(() => { void feed.loadMore(); void feed.loadMore() })
  expect(pending).toHaveLength(2)
  await resolve(1, ['b', 'c'])
  expect(feed.events.map((e) => e.event_id)).toEqual(['a', 'b', 'c'])
  expect(feed.loadingMore).toBe(false)
})

it('keeps loaded pages and their cursor when refreshing the first page', async () => {
  await resolve(0, ['a', 'b'], 'older')
  act(() => { void feed.loadMore() })
  await resolve(1, ['c', 'd'], 'oldest')
  act(() => root.render(<Probe refresh={1} />))
  expect(feed.status).toBe('success')
  await resolve(2, ['new', 'a'], 'different')
  expect(feed.events.map((e) => e.event_id)).toEqual(['new', 'a', 'b', 'c', 'd'])
  expect(feed.nextCursor).toBe('oldest')
})

it('does not leak a stale pagination error or clear a newer request', async () => {
  await resolve(0, ['old'], 'more')
  act(() => { void feed.loadMore() })
  act(() => root.render(<Probe model="new" />))
  await resolve(2, ['new'], 'more-new')
  act(() => { void feed.loadMore() })
  await act(async () => pending[1].reject(new Error('old failure')))
  expect(feed.loadMoreError).toBeNull()
  expect(feed.loadingMore).toBe(true)
  await resolve(3, ['newer-page'])
  expect(feed.loadingMore).toBe(false)
})

it('reports failure when returning to a previous query while another loads', async () => {
  await resolve(0, ['a'])
  act(() => root.render(<Probe model="other" />))
  act(() => root.render(<Probe />))
  await act(async () => pending[2].reject(new Error('current failure')))
  expect(feed.status).toBe('error')
  expect(feed.error).toBe('current failure')
})

it('preserves current data on a failed background refresh', async () => {
  await resolve(0, ['a'])
  act(() => root.render(<Probe refresh={1} />))
  await act(async () => pending[1].reject(new Error('temporary')))
  expect(feed.status).toBe('success')
  expect(feed.events[0].event_id).toBe('a')
})
