import { Folder20Regular } from '@fluentui/react-icons/headless/svg/folder'
import { getBreakdown } from '../api/client'
import type { BreakdownRow } from '../api/types'
import { type Column, DataTable } from '../components/DataTable'
import { costColumn, countColumn, dateTimeColumn, tokenColumn } from '../components/columns'
import { PageHeader } from '../components/PageHeader'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { labelForKey } from '../lib/colors'
import { shortenProjectKey } from '../lib/format'
import { tokensOf } from '../lib/usage'
import { useFilters } from '../state/FiltersContext'

/** Last path segment of a cwd or workspace URI ("C:\a\b" and "file:///a/b" → "b"). */
function projectName(key: string): string {
  const segments = key.replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, '').split(/[\\/]/).filter(Boolean)
  return segments[segments.length - 1] ?? key
}

// File-list layout: folder icon, folder name, the full location underneath.
const PROJECT_COLUMNS: Column<BreakdownRow>[] = [
  {
    key: 'project',
    header: '프로젝트',
    media: () => <Folder20Regular />,
    render: (r) => <span title={r.key}>{projectName(r.key)}</span>,
    description: (r) => <span title={r.key}>{shortenProjectKey(r.key)}</span>,
    sortValue: (r) => r.key,
  },
  { key: 'source', header: '소스', render: (r) => labelForKey(r.source ?? '-'), sortValue: (r) => r.source ?? '' },
  costColumn('cost', '비용', (r) => r.cost_usd, { badge: true, ratio: (r) => r.cost_known_ratio }),
  tokenColumn('tokens', '토큰', tokensOf),
  countColumn('events', '이벤트', (r) => r.events),
  dateTimeColumn('last_seen', '최근 사용', (r) => r.last_seen),
]

export function Projects() {
  const { range, refreshKey } = useFilters()
  const state = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'project', limit: 200 }),
    [range.from, range.to, refreshKey],
  )

  return (
    <div>
      <PageHeader title="프로젝트" />
      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.rows.length === 0}
        emptyMessage="해당 기간에 프로젝트 사용 내역이 없습니다."
      >
        {state.status === 'success' && (
          <DataTable columns={PROJECT_COLUMNS} rows={state.data.rows} rowKey={(r) => `${r.source}:${r.key}`} initialSortKey="cost" />
        )}
      </StateContainer>
    </div>
  )
}
