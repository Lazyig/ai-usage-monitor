import { getBreakdown } from '../api/client'
import type { BreakdownRow } from '../api/types'
import { Badge } from '../components/Badge'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { labelForKey } from '../lib/colors'
import { formatCompact, formatDateTime, formatFull, formatUsd, shortenProjectKey } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

export function Projects() {
  const { range, refreshKey } = useFilters()
  const state = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'project', limit: 200 }),
    [range.from, range.to, refreshKey],
  )

  const columns: Column<BreakdownRow>[] = [
    {
      key: 'project',
      header: '프로젝트',
      render: (r) => <span title={r.key}>{shortenProjectKey(r.key)}</span>,
      sortValue: (r) => r.key,
    },
    { key: 'source', header: '소스', render: (r) => labelForKey(r.source ?? '-'), sortValue: (r) => r.source ?? '' },
    {
      key: 'cost',
      header: '비용',
      align: 'right',
      render: (r) => (
        <span>
          {formatUsd(r.cost_usd)} {r.cost_usd === null && <Badge kind="unknown" />}
        </span>
      ),
      sortValue: (r) => r.cost_usd ?? -1,
    },
    {
      key: 'tokens',
      header: '토큰',
      align: 'right',
      render: (r) => (
        <span title={formatFull(r.input + r.output + r.cache_read + r.cache_write)}>
          {formatCompact(r.input + r.output + r.cache_read + r.cache_write)}
        </span>
      ),
      sortValue: (r) => r.input + r.output + r.cache_read + r.cache_write,
    },
    { key: 'events', header: '이벤트', align: 'right', render: (r) => formatFull(r.events), sortValue: (r) => r.events },
    { key: 'last_seen', header: '최근 사용', render: (r) => formatDateTime(r.last_seen), sortValue: (r) => r.last_seen },
  ]

  return (
    <div>
      <div className="page-header">
        <h1>프로젝트</h1>
      </div>
      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.rows.length === 0}
        emptyMessage="해당 기간에 프로젝트 사용 내역이 없습니다."
      >
        {state.status === 'success' && <DataTable columns={columns} rows={state.data.rows} rowKey={(r) => `${r.source}:${r.key}`} initialSortKey="cost" />}
      </StateContainer>
    </div>
  )
}
