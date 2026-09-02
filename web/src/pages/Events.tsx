import { useEffect, useState } from 'react'
import { getDevices, getEvents } from '../api/client'
import type { Device, Source, UsageEventDTO } from '../api/types'
import { type Column, DataTable } from '../components/DataTable'
import { costColumn, tokenColumn, unsorted } from '../components/columns'
import { StateContainer } from '../components/StateContainer'
import { SOURCE_ORDER, labelForKey } from '../lib/colors'
import { describeError } from '../lib/errors'
import { formatDateTime } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

const PAGE_SIZE = 100

export function Events() {
  const { range, refreshKey } = useFilters()
  const [source, setSource] = useState<Source | ''>('')
  const [device, setDevice] = useState('')
  const [model, setModel] = useState('')
  const [devices, setDevices] = useState<Device[]>([])

  const [events, setEvents] = useState<UsageEventDTO[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>(undefined)
  const [status, setStatus] = useState<'loading' | 'error' | 'success'>('loading')
  const [error, setError] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)

  useEffect(() => {
    getDevices()
      .then((r) => setDevices(r.devices))
      .catch(() => undefined)
  }, [])

  useEffect(() => {
    let cancelled = false
    setStatus('loading')
    setEvents([])
    setNextCursor(undefined)
    getEvents({
      from: range.from,
      to: range.to,
      source: source || undefined,
      device: device || undefined,
      model: model || undefined,
      limit: PAGE_SIZE,
    })
      .then((r) => {
        if (cancelled) return
        setEvents(r.events)
        setNextCursor(r.next_cursor)
        setStatus('success')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(describeError(err))
        setStatus('error')
      })
    return () => {
      cancelled = true
    }
  }, [range.from, range.to, source, device, model, refreshKey])

  const [loadMoreError, setLoadMoreError] = useState<string | null>(null)

  async function loadMore() {
    if (!nextCursor) return
    setLoadingMore(true)
    setLoadMoreError(null)
    try {
      const r = await getEvents({
        from: range.from,
        to: range.to,
        source: source || undefined,
        device: device || undefined,
        model: model || undefined,
        limit: PAGE_SIZE,
        cursor: nextCursor,
      })
      setEvents((prev) => [...prev, ...r.events])
      setNextCursor(r.next_cursor)
    } catch (err) {
      setLoadMoreError(describeError(err))
    } finally {
      setLoadingMore(false)
    }
  }

  // The event log is already newest-first from the server and paginates by
  // cursor, so no column offers client-side sorting — re-sorting one loaded
  // page would silently misrepresent the whole result set.
  const columns: Column<UsageEventDTO>[] = [
    { key: 'ts', header: '시각', render: (e) => formatDateTime(e.ts) },
    { key: 'source', header: '소스', render: (e) => labelForKey(e.source) },
    { key: 'device', header: '기기', render: (e) => devices.find((d) => d.id === e.device_id)?.name ?? e.device_id.slice(0, 8) },
    { key: 'model', header: '모델', render: (e) => e.model },
    { key: 'project', header: '프로젝트', render: (e) => <span title={e.project}>{e.project ?? '-'}</span> },
    unsorted(tokenColumn<UsageEventDTO>('input', '입력', (e) => e.input)),
    unsorted(tokenColumn<UsageEventDTO>('output', '출력', (e) => e.output)),
    unsorted(costColumn<UsageEventDTO>('cost', '비용', (e) => e.cost_usd)),
    { key: 'confidence', header: '신뢰도', render: (e) => e.confidence },
  ]

  return (
    <div>
      <div className="page-header">
        <h1>이벤트</h1>
      </div>

      <div className="filters-row">
        <div className="field">
          <label htmlFor="ev-source">소스</label>
          <select id="ev-source" value={source} onChange={(e) => setSource(e.target.value as Source | '')}>
            <option value="">전체</option>
            {SOURCE_ORDER.map((s) => (
              <option key={s} value={s}>
                {labelForKey(s)}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ev-device">기기</label>
          <select id="ev-device" value={device} onChange={(e) => setDevice(e.target.value)}>
            <option value="">전체</option>
            {devices.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ev-model">모델</label>
          <input id="ev-model" value={model} onChange={(e) => setModel(e.target.value)} placeholder="모델명 필터" />
        </div>
      </div>

      <StateContainer loading={status === 'loading'} error={status === 'error' ? error : null} empty={status === 'success' && events.length === 0}>
        <DataTable columns={columns} rows={events} rowKey={(e) => e.event_id} />
        {loadMoreError && (
          <p className="login-error" role="alert" style={{ textAlign: 'center', marginTop: 8 }}>
            추가 이벤트를 불러오지 못했습니다: {loadMoreError}
          </p>
        )}
        {nextCursor && (
          <div style={{ textAlign: 'center', marginTop: 12 }}>
            <button type="button" className="btn" onClick={loadMore} disabled={loadingMore}>
              {loadingMore ? '불러오는 중…' : '더 보기'}
            </button>
          </div>
        )}
      </StateContainer>
    </div>
  )
}
