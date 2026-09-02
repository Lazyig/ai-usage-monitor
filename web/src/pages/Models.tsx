import { useMemo, useState } from 'react'
import { Bar, BarChart, CartesianGrid, Cell, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { getBreakdown } from '../api/client'
import type { BreakdownRow, Source } from '../api/types'
import { Badge } from '../components/Badge'
import { ChartTooltip } from '../components/ChartTooltip'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { labelForKey } from '../lib/colors'
import { formatCompact, formatFull, formatUsd } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

const SOURCES: Source[] = ['claude_code', 'codex', 'antigravity']

export function Models() {
  const { range, refreshKey } = useFilters()
  const [source, setSource] = useState<Source | ''>('')

  const state = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'model', source: source || undefined, limit: 100 }),
    [range.from, range.to, source, refreshKey],
  )

  const chartRows = useMemo(() => {
    if (state.status !== 'success') return []
    return [...state.data.rows]
      .filter((r) => r.cost_usd !== null)
      .sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0))
      .slice(0, 12)
  }, [state])

  return (
    <div>
      <div className="page-header">
        <h1>모델</h1>
      </div>

      <div className="filters-row">
        <div className="field">
          <label htmlFor="source-filter">소스</label>
          <select id="source-filter" value={source} onChange={(e) => setSource(e.target.value as Source | '')}>
            <option value="">전체</option>
            {SOURCES.map((s) => (
              <option key={s} value={s}>
                {labelForKey(s)}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="chart-card">
        <h2>모델별 비용 (상위 12개)</h2>
        <StateContainer
          loading={state.status === 'loading'}
          error={state.status === 'error' ? state.message : null}
          empty={state.status === 'success' && chartRows.length === 0}
          emptyMessage="가격이 알려진 모델 사용 내역이 없습니다."
        >
          {chartRows.length > 0 && <ModelCostBarChart rows={chartRows} />}
        </StateContainer>
      </div>

      <h2>전체 모델</h2>
      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.rows.length === 0}
      >
        {state.status === 'success' && <ModelsTable rows={state.data.rows} />}
      </StateContainer>
    </div>
  )
}

function ModelCostBarChart({ rows }: { rows: BreakdownRow[] }) {
  const data = rows.map((r) => ({ name: r.key, cost: r.cost_usd ?? 0 }))
  return (
    <ResponsiveContainer width="100%" height={Math.max(180, data.length * 34)}>
      <BarChart data={data} layout="vertical" margin={{ left: 8, right: 16 }}>
        <CartesianGrid stroke="var(--border)" horizontal={false} />
        <XAxis type="number" tickFormatter={(v: number) => formatUsd(v)} tick={{ fill: 'var(--text-muted)', fontSize: 11 }} axisLine={false} tickLine={false} />
        <YAxis type="category" dataKey="name" tick={{ fill: 'var(--text-secondary)', fontSize: 12 }} axisLine={false} tickLine={false} width={140} />
        <Tooltip content={<ChartTooltip formatValue={formatUsd} />} cursor={{ fill: 'var(--border)', opacity: 0.4 }} />
        <Bar dataKey="cost" radius={[0, 4, 4, 0]} maxBarSize={18}>
          {data.map((d) => (
            <Cell key={d.name} fill="var(--series-1)" />
          ))}
        </Bar>
      </BarChart>
    </ResponsiveContainer>
  )
}

function ModelsTable({ rows }: { rows: BreakdownRow[] }) {
  const columns: Column<BreakdownRow>[] = [
    {
      key: 'model',
      header: '모델',
      render: (r) => (
        <span>
          {r.key} {r.estimated && <Badge kind="estimated" />} {r.cost_usd === null && <Badge kind="unknown" />}
        </span>
      ),
      sortValue: (r) => r.key,
    },
    { key: 'source', header: '소스', render: (r) => labelForKey(r.source ?? '-'), sortValue: (r) => r.source ?? '' },
    { key: 'cost', header: '비용', align: 'right', render: (r) => formatUsd(r.cost_usd), sortValue: (r) => r.cost_usd ?? -1 },
    { key: 'input', header: '입력', align: 'right', render: (r) => <span title={formatFull(r.input)}>{formatCompact(r.input)}</span>, sortValue: (r) => r.input },
    { key: 'output', header: '출력', align: 'right', render: (r) => <span title={formatFull(r.output)}>{formatCompact(r.output)}</span>, sortValue: (r) => r.output },
    {
      key: 'cache_read',
      header: '캐시 읽기',
      align: 'right',
      render: (r) => <span title={formatFull(r.cache_read)}>{formatCompact(r.cache_read)}</span>,
      sortValue: (r) => r.cache_read,
    },
    {
      key: 'cache_write',
      header: '캐시 쓰기',
      align: 'right',
      render: (r) => <span title={formatFull(r.cache_write)}>{formatCompact(r.cache_write)}</span>,
      sortValue: (r) => r.cache_write,
    },
    { key: 'events', header: '이벤트', align: 'right', render: (r) => formatFull(r.events), sortValue: (r) => r.events },
  ]
  return rows.length === 0 ? (
    <p className="text-muted">해당 기간에 데이터가 없습니다.</p>
  ) : (
    <DataTable columns={columns} rows={rows} rowKey={(r) => `${r.source}:${r.key}`} initialSortKey="cost" />
  )
}
