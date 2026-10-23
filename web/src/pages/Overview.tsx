import { useState, type ReactNode } from 'react'
import { getSummary, getTimeseries } from '../api/client'
import type { SummaryDeviceRow, SummaryModelRow, SummaryResponse, SummarySourceRow } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { type Column, DataTable } from '../components/DataTable'
import { Desktop20Regular } from '@fluentui/react-icons/headless/svg/desktop'
import { costColumn, countColumn, Swatch, tokenColumn } from '../components/columns'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SegmentedControl } from '../components/SegmentedControl'
import { StateContainer } from '../components/StateContainer'
import { StatTile } from '../components/StatTile'
import { TrendChart } from '../components/TrendChart'
import { type ApiState, useApiData } from '../hooks/useApiData'
import { type Metric, METRIC_OPTIONS } from '../lib/chartData'
import { colorForKey, labelForKey } from '../lib/colors'
import { formatCompact, formatFull, formatPercent, formatUsd } from '../lib/format'
import { tokensOf } from '../lib/usage'
import { useFilters } from '../state/FiltersContext'

/** Below this share of priced tokens the headline cost is materially
 * incomplete, so it is flagged rather than presented as the total. */
const COST_KNOWN_THRESHOLD = 0.95

export function Overview() {
  const { range, refreshKey } = useFilters()
  const [chartMode, setChartMode] = useState<Metric>('cost')

  const summaryState = useApiData(() => getSummary({ from: range.from, to: range.to }), [range.from, range.to, refreshKey])
  const timeseriesState = useApiData(
    () => getTimeseries({ from: range.from, to: range.to, bucket: 'day', group_by: 'source' }),
    [range.from, range.to, refreshKey],
  )

  const summaryError = summaryState.status === 'error' ? summaryState.message : null
  const totals = summaryState.status === 'success' ? summaryState.data.totals : null
  const costIncomplete = totals !== null && totals.cost_known_ratio < COST_KNOWN_THRESHOLD

  return (
    <div>
      <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
        <h1>개요</h1>
      </div>

      <ErrorBoundary label="통계 타일을 표시할 수 없습니다">
        <StateContainer loading={summaryState.status === 'loading'} error={summaryError} onRetry={summaryState.reload}>
          {totals && (
            <div className="mb-6 grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-3 max-tablet:grid-cols-[repeat(2,1fr)] max-tiny:grid-cols-[1fr]">
              <StatTile
                label="총 비용"
                value={formatUsd(totals.cost_usd)}
                hint={
                  costIncomplete ? (
                    <Badge kind="warning">가격 반영 {formatPercent(totals.cost_known_ratio)}</Badge>
                  ) : (
                    `가격 반영 ${formatPercent(totals.cost_known_ratio)}`
                  )
                }
                warn={costIncomplete}
              />
              <StatTile label="이벤트" value={formatFull(totals.events)} />
              <StatTile label="입력 토큰" value={formatCompact(totals.input)} title={formatFull(totals.input)} />
              <StatTile label="출력 토큰" value={formatCompact(totals.output)} title={formatFull(totals.output)} />
              <StatTile label="캐시 읽기" value={formatCompact(totals.cache_read)} title={formatFull(totals.cache_read)} />
              <StatTile label="캐시 쓰기" value={formatCompact(totals.cache_write)} title={formatFull(totals.cache_write)} />
            </div>
          )}
        </StateContainer>
      </ErrorBoundary>

      <Card
        className="mb-6"
        title="소스별 사용량 추이"
        controls={<SegmentedControl ariaLabel="차트 값 전환" value={chartMode} onChange={setChartMode} options={METRIC_OPTIONS} />}
      >
        <ErrorBoundary label="차트를 표시할 수 없습니다">
          <StateContainer
            loading={timeseriesState.status === 'loading'}
            error={timeseriesState.status === 'error' ? timeseriesState.message : null}
            onRetry={timeseriesState.reload}
            empty={timeseriesState.status === 'success' && timeseriesState.data.series.length === 0}
          >
            {timeseriesState.status === 'success' && (
              <TrendChart
                series={timeseriesState.data.series}
                mode={chartMode}
                colorFor={colorForKey}
                height={280}
                barStroke={false}
                span={{ range, bucket: timeseriesState.data.bucket }}
              />
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.35fr)] gap-4 *:min-w-0 max-wide:grid-cols-[repeat(2,minmax(0,1fr))] max-wide:[&>:nth-child(3)]:col-[1/-1] max-narrow:grid-cols-[1fr]">
        <SummaryPanel title="소스별" label="소스별 표를 표시할 수 없습니다" state={summaryState}>
          {(data) => <SummaryTable columns={SOURCE_COLUMNS} rows={data.by_source} rowKey={(r) => r.key} />}
        </SummaryPanel>
        <SummaryPanel title="기기별" label="기기별 표를 표시할 수 없습니다" state={summaryState}>
          {(data) => <SummaryTable columns={DEVICE_COLUMNS} rows={data.by_device} rowKey={(r) => r.key} />}
        </SummaryPanel>
        <SummaryPanel title="모델별" label="모델별 표를 표시할 수 없습니다" state={summaryState}>
          {(data) => <SummaryTable columns={MODEL_COLUMNS} rows={data.by_model} rowKey={(r) => `${r.source}:${r.key}`} />}
        </SummaryPanel>
      </div>
    </div>
  )
}

type SummaryState = ApiState<SummaryResponse> & { reload: () => void }

/** One of the three side-by-side summary tables, each wrapped in its own
 * boundary so a single bad table never takes the row down. */
function SummaryPanel({
  title,
  label,
  state,
  children,
}: {
  title: string
  label: string
  state: SummaryState
  children: (data: SummaryResponse) => ReactNode
}) {
  return (
    <Card title={title}>
      <ErrorBoundary label={label}>
        <StateContainer
          loading={state.status === 'loading'}
          error={state.status === 'error' ? state.message : null}
          onRetry={state.reload}
        >
          {state.status === 'success' && children(state.data)}
        </StateContainer>
      </ErrorBoundary>
    </Card>
  )
}

function SummaryTable<T>({ columns, rows, rowKey }: { columns: Column<T>[]; rows: T[]; rowKey: (row: T) => string }) {
  return rows.length === 0 ? (
    <p className="text-fg3">데이터가 없습니다.</p>
  ) : (
    <DataTable columns={columns} rows={rows} rowKey={rowKey} initialSortKey="cost" />
  )
}

const SOURCE_COLUMNS: Column<SummarySourceRow>[] = [
  {
    key: 'key',
    header: '소스',
    media: (r) => <Swatch color={colorForKey(r.key, 0)} />,
    render: (r) => labelForKey(r.key),
    sortValue: (r) => labelForKey(r.key),
  },
  costColumn('cost', '비용', (r) => r.cost_usd, { ratio: (r) => r.cost_known_ratio }),
  tokenColumn('tokens', '토큰', tokensOf),
  countColumn('events', '이벤트', (r) => r.events),
]

const DEVICE_COLUMNS: Column<SummaryDeviceRow>[] = [
  { key: 'name', header: '기기', media: () => <Desktop20Regular />, render: (r) => r.name, sortValue: (r) => r.name },
  costColumn('cost', '비용', (r) => r.cost_usd, { ratio: (r) => r.cost_known_ratio }),
  tokenColumn('tokens', '토큰', tokensOf),
  countColumn('events', '이벤트', (r) => r.events),
]

const MODEL_COLUMNS: Column<SummaryModelRow>[] = [
  {
    key: 'model',
    header: '모델',
    media: (r) => <Swatch color={colorForKey(r.source, 0)} />,
    render: (r) => (
      <span>
        {r.key} {r.estimated && <Badge kind="estimated" />} {r.cost_usd === null && <Badge kind="unknown" />}
      </span>
    ),
    sortValue: (r) => r.key,
  },
  { key: 'source', header: '소스', render: (r) => labelForKey(r.source), sortValue: (r) => r.source },
  costColumn('cost', '비용', (r) => r.cost_usd, { ratio: (r) => r.cost_known_ratio }),
  countColumn('events', '이벤트', (r) => r.events),
]
