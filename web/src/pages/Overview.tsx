import { useState } from 'react'
import { Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { getSummary, getTimeseries } from '../api/client'
import type { SummaryDeviceRow, SummaryModelRow, SummarySourceRow } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { ChartTooltip } from '../components/ChartTooltip'
import { type Column, DataTable } from '../components/DataTable'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SegmentedControl } from '../components/SegmentedControl'
import { StateContainer } from '../components/StateContainer'
import { StatTile } from '../components/StatTile'
import { useApiData } from '../hooks/useApiData'
import { colorForKey, labelForKey } from '../lib/colors'
import { costOfPoint, formatBucketLabel, mergeSeries, tokensOfPoint } from '../lib/chartData'
import { formatCompact, formatFull, formatPercent, formatUsd } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

const COST_KNOWN_THRESHOLD = 0.95

export function Overview() {
  const { range, refreshKey } = useFilters()
  const [chartMode, setChartMode] = useState<'cost' | 'tokens'>('cost')

  const summaryState = useApiData(() => getSummary({ from: range.from, to: range.to }), [range.from, range.to, refreshKey])
  const timeseriesState = useApiData(
    () => getTimeseries({ from: range.from, to: range.to, bucket: 'day', group_by: 'source' }),
    [range.from, range.to, refreshKey],
  )

  return (
    <div>
      <div className="page-header">
        <h1>개요</h1>
      </div>

      <ErrorBoundary label="통계 타일을 표시할 수 없습니다">
        <StateContainer
          loading={summaryState.status === 'loading'}
          error={summaryState.status === 'error' ? summaryState.message : null}
          onRetry={summaryState.reload}
        >
          {summaryState.status === 'success' && (
            <div className="stat-tiles">
              <StatTile
                label="총 비용"
                value={formatUsd(summaryState.data.totals.cost_usd)}
                hint={
                  summaryState.data.totals.cost_known_ratio < COST_KNOWN_THRESHOLD ? (
                    <Badge kind="warning">가격 반영 {formatPercent(summaryState.data.totals.cost_known_ratio)}</Badge>
                  ) : (
                    `가격 반영 ${formatPercent(summaryState.data.totals.cost_known_ratio)}`
                  )
                }
                warn={summaryState.data.totals.cost_known_ratio < COST_KNOWN_THRESHOLD}
              />
              <StatTile label="이벤트" value={formatFull(summaryState.data.totals.events)} />
              <StatTile
                label="입력 토큰"
                value={formatCompact(summaryState.data.totals.input)}
                title={formatFull(summaryState.data.totals.input)}
              />
              <StatTile
                label="출력 토큰"
                value={formatCompact(summaryState.data.totals.output)}
                title={formatFull(summaryState.data.totals.output)}
              />
              <StatTile
                label="캐시 읽기"
                value={formatCompact(summaryState.data.totals.cache_read)}
                title={formatFull(summaryState.data.totals.cache_read)}
              />
              <StatTile
                label="캐시 쓰기"
                value={formatCompact(summaryState.data.totals.cache_write)}
                title={formatFull(summaryState.data.totals.cache_write)}
              />
            </div>
          )}
        </StateContainer>
      </ErrorBoundary>

      <Card
        className="section"
        title="소스별 사용량 추이"
        controls={
          <SegmentedControl
            ariaLabel="차트 값 전환"
            value={chartMode}
            onChange={setChartMode}
            options={[
              { value: 'cost', label: '비용' },
              { value: 'tokens', label: '토큰' },
            ]}
          />
        }
      >
        <ErrorBoundary label="차트를 표시할 수 없습니다">
          <StateContainer
            loading={timeseriesState.status === 'loading'}
            error={timeseriesState.status === 'error' ? timeseriesState.message : null}
            onRetry={timeseriesState.reload}
            empty={timeseriesState.status === 'success' && timeseriesState.data.series.length === 0}
          >
            {timeseriesState.status === 'success' && <TimeseriesChart series={timeseriesState.data.series} mode={chartMode} />}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <div className="grid grid-cols-3">
        <div>
          <h2>소스별</h2>
          <ErrorBoundary label="소스별 표를 표시할 수 없습니다">
            <StateContainer
              loading={summaryState.status === 'loading'}
              error={summaryState.status === 'error' ? summaryState.message : null}
              onRetry={summaryState.reload}
            >
              {summaryState.status === 'success' && <SourceTable rows={summaryState.data.by_source} />}
            </StateContainer>
          </ErrorBoundary>
        </div>
        <div>
          <h2>기기별</h2>
          <ErrorBoundary label="기기별 표를 표시할 수 없습니다">
            <StateContainer
              loading={summaryState.status === 'loading'}
              error={summaryState.status === 'error' ? summaryState.message : null}
              onRetry={summaryState.reload}
            >
              {summaryState.status === 'success' && <DeviceTable rows={summaryState.data.by_device} />}
            </StateContainer>
          </ErrorBoundary>
        </div>
        <div>
          <h2>모델별</h2>
          <ErrorBoundary label="모델별 표를 표시할 수 없습니다">
            <StateContainer
              loading={summaryState.status === 'loading'}
              error={summaryState.status === 'error' ? summaryState.message : null}
              onRetry={summaryState.reload}
            >
              {summaryState.status === 'success' && <ModelTable rows={summaryState.data.by_model} />}
            </StateContainer>
          </ErrorBoundary>
        </div>
      </div>
    </div>
  )
}

function TimeseriesChart({ series, mode }: { series: import('../api/types').TimeseriesSeries[]; mode: 'cost' | 'tokens' }) {
  const { data, keys } = mergeSeries(series, mode === 'cost' ? costOfPoint : tokensOfPoint)
  if (data.length === 0) return null
  return (
    <ResponsiveContainer width="100%" height={280}>
      <BarChart data={data} barCategoryGap={4} margin={{ left: 4, right: 4 }}>
        <CartesianGrid stroke="var(--border)" vertical={false} />
        <XAxis
          dataKey="t"
          tickFormatter={formatBucketLabel}
          tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
          axisLine={{ stroke: 'var(--border-strong)' }}
          tickLine={false}
        />
        <YAxis
          tickFormatter={(v: number) => (mode === 'cost' ? formatUsd(v) : formatCompact(v))}
          tick={{ fill: 'var(--text-muted)', fontSize: 11 }}
          axisLine={false}
          tickLine={false}
          width={64}
        />
        <Tooltip content={<ChartTooltip formatValue={mode === 'cost' ? formatUsd : formatCompact} />} cursor={{ fill: 'var(--border)', opacity: 0.4 }} />
        <Legend
          formatter={(value: string) => <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>{labelForKey(value)}</span>}
        />
        {keys.map((key, i) => (
          <Bar key={key} dataKey={key} stackId="usage" fill={colorForKey(key, i)} radius={[2, 2, 2, 2]} maxBarSize={28} />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}

function SourceTable({ rows }: { rows: SummarySourceRow[] }) {
  const columns: Column<SummarySourceRow>[] = [
    { key: 'key', header: '소스', render: (r) => labelForKey(r.key), sortValue: (r) => labelForKey(r.key) },
    { key: 'cost', header: '비용', align: 'right', render: (r) => formatUsd(r.cost_usd), sortValue: (r) => r.cost_usd ?? -1 },
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
  ]
  return rows.length === 0 ? (
    <p className="text-muted">데이터가 없습니다.</p>
  ) : (
    <DataTable columns={columns} rows={rows} rowKey={(r) => r.key} initialSortKey="cost" />
  )
}

function DeviceTable({ rows }: { rows: SummaryDeviceRow[] }) {
  const columns: Column<SummaryDeviceRow>[] = [
    { key: 'name', header: '기기', render: (r) => r.name, sortValue: (r) => r.name },
    { key: 'cost', header: '비용', align: 'right', render: (r) => formatUsd(r.cost_usd), sortValue: (r) => r.cost_usd ?? -1 },
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
  ]
  return rows.length === 0 ? (
    <p className="text-muted">데이터가 없습니다.</p>
  ) : (
    <DataTable columns={columns} rows={rows} rowKey={(r) => r.key} initialSortKey="cost" />
  )
}

function ModelTable({ rows }: { rows: SummaryModelRow[] }) {
  const columns: Column<SummaryModelRow>[] = [
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
    { key: 'source', header: '소스', render: (r) => labelForKey(r.source), sortValue: (r) => r.source },
    { key: 'cost', header: '비용', align: 'right', render: (r) => formatUsd(r.cost_usd), sortValue: (r) => r.cost_usd ?? -1 },
    { key: 'events', header: '이벤트', align: 'right', render: (r) => formatFull(r.events), sortValue: (r) => r.events },
  ]
  return rows.length === 0 ? (
    <p className="text-muted">데이터가 없습니다.</p>
  ) : (
    <DataTable columns={columns} rows={rows} rowKey={(r) => `${r.source}:${r.key}`} initialSortKey="cost" />
  )
}
