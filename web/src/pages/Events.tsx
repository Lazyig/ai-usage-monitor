import { useEffect, useState } from 'react'
import { getDevices } from '../api/client'
import type { Device, Source, UsageEventDTO } from '../api/types'
import { type Column, DataTable } from '../components/DataTable'
import { RowField } from '../components/RowField'
import { StateContainer } from '../components/StateContainer'
import { costColumn, Swatch, tokenColumn, unsorted } from '../components/columns'
import { Button } from '../components/ui/button'
import { Input } from '../components/ui/input'
import { Select } from '../components/ui/select'
import { useEventFeed } from '../hooks/useEventFeed'
import { SOURCE_ORDER, colorForKey, labelForKey } from '../lib/colors'
import { formatDateTime } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

export function Events() {
  const { range, refreshKey } = useFilters()
  const [source, setSource] = useState<Source | ''>('')
  const [device, setDevice] = useState('')
  const [model, setModel] = useState('')
  const [devices, setDevices] = useState<Device[]>([])

  const { events, nextCursor, status, error, loadingMore, loadMoreError, loadMore } = useEventFeed({
    from: range.from, to: range.to,
    source: source || undefined, device: device || undefined, model: model || undefined,
  }, refreshKey)

  useEffect(() => {
    getDevices()
      .then((r) => setDevices(r.devices))
      .catch(() => undefined)
  }, [])

  // The event log is already newest-first from the server and paginates by
  // cursor, so no column offers client-side sorting — re-sorting one loaded
  // page would silently misrepresent the whole result set.
  const columns: Column<UsageEventDTO>[] = [
    { key: 'ts', header: '시각', render: (e) => formatDateTime(e.ts) },
    { key: 'source', header: '소스', media: (e) => <Swatch color={colorForKey(e.source, 0)} />, render: (e) => labelForKey(e.source) },
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
      <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
        <h1>이벤트</h1>
      </div>

      <div className="mb-4 flex flex-wrap items-end gap-3 max-phone:flex-col max-phone:items-stretch">
        <RowField label="소스">
          <Select
            id="ev-source"
            value={source}
            onValueChange={(v) => setSource(v as Source | '')}
            options={[{ value: '', label: '전체' }, ...SOURCE_ORDER.map((s) => ({ value: s, label: labelForKey(s) }))]}
          />
        </RowField>
        <RowField label="기기">
          <Select
            id="ev-device"
            value={device}
            onValueChange={(v) => setDevice(v)}
            options={[{ value: '', label: '전체' }, ...devices.map((d) => ({ value: d.id, label: d.name }))]}
          />
        </RowField>
        <RowField label="모델">
          <Input id="ev-model" value={model} onChange={(e) => setModel(e.target.value)} placeholder="모델명 필터" />
        </RowField>
      </div>

      <StateContainer loading={status === 'loading'} error={status === 'error' ? error : null} empty={status === 'success' && events.length === 0}>
        <DataTable columns={columns} rows={events} rowKey={(e) => e.event_id} size="small" />
        {loadMoreError && (
          <p className="text-200 text-danger" role="alert" style={{ textAlign: 'center', marginTop: 8 }}>
            추가 이벤트를 불러오지 못했습니다: {loadMoreError}
          </p>
        )}
        {nextCursor && (
          <div style={{ textAlign: 'center', marginTop: 12 }}>
            <Button touch onClick={loadMore} disabled={loadingMore}>
              {loadingMore ? '불러오는 중…' : '더 보기'}
            </Button>
          </div>
        )}
      </StateContainer>
    </div>
  )
}
