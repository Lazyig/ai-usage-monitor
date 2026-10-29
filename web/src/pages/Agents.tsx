import { useMemo, useState } from 'react'
import { getBreakdown, getDevices, getThroughput, getTimeseries } from '../api/client'
import type { BreakdownRow, Source, TimeseriesBucket } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { CompositionChart, CompositionLegend, type CompositionSegment } from '../components/CompositionChart'
import { type Column, DataTable } from '../components/DataTable'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { RowField } from '../components/RowField'
import { SegmentedControl } from '../components/SegmentedControl'
import { StateContainer } from '../components/StateContainer'
import { ThroughputCard } from '../components/ThroughputCard'
import { TrendChart } from '../components/TrendChart'
import { costColumn, countColumn, dateTimeColumn, Swatch, tokenColumn } from '../components/columns'
import { Button } from '../components/ui/button'
import { Select } from '../components/ui/select'
import { useApiData } from '../hooks/useApiData'
import { type Metric, METRIC_OPTIONS, BUCKET_OPTIONS, metricFormat } from '../lib/chartData'
import { SOURCE_COLOR_VARS, SOURCE_ORDER, colorForSlot, labelForKey } from '../lib/colors'
import { hourlyAllowed } from '../lib/dateRange'
import { formatCompact, formatFull, formatPercent, formatUsd } from '../lib/format'
import { buildMatrix, fetchSourceDeviceMatrix } from '../lib/matrix'
import { cacheHitRatio, tokensOf } from '../lib/usage'
import { useFilters } from '../state/FiltersContext'

/**
 * Per-agent view: how the three collectors (Claude Code / Codex / Antigravity)
 * compare in the selected range — headline totals, the daily/hourly trend, the
 * device each agent's usage came from, and the full per-agent table. The device
 * filter narrows every card on the page.
 */
export function Agents() {
  const { range, refreshKey } = useFilters()
  const [device, setDevice] = useState('')
  const [summaryMetric, setSummaryMetric] = useState<Metric>('cost')
  const [trendMetric, setTrendMetric] = useState<Metric>('cost')
  const [bucket, setBucket] = useState<TimeseriesBucket>('day')
  const [mixMetric, setMixMetric] = useState<Metric>('cost')

  const canBucketHourly = hourlyAllowed(range)
  const effectiveBucket: TimeseriesBucket = canBucketHourly ? bucket : 'day'

  const devicesState = useApiData(() => getDevices(), [refreshKey])
  const summaryState = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'source', device: device || undefined, limit: 50 }),
    [range.from, range.to, device, refreshKey],
  )
  const trendState = useApiData(
    () => getTimeseries({ from: range.from, to: range.to, bucket: effectiveBucket, group_by: 'source', device: device || undefined }),
    [range.from, range.to, effectiveBucket, device, refreshKey],
  )
  const throughputState = useApiData(
    () => getThroughput({ from: range.from, to: range.to, group_by: 'source', device: device || undefined }),
    [range.from, range.to, device, refreshKey],
  )
  // The agent × device mix only says something while every device is in play;
  // with one device selected it would be a single band, so it is not fetched.
  const mixState = useApiData(
    () => (device ? Promise.resolve([]) : fetchSourceDeviceMatrix({ from: range.from, to: range.to, sources: SOURCE_ORDER })),
    [range.from, range.to, device, refreshKey],
  )

  const rows = summaryState.status === 'success' ? summaryState.data.rows : []
  const byKey = useMemo(() => new Map(rows.map((r) => [r.key, r])), [rows])
  const totals = useMemo(
    () => ({
      cost: rows.reduce((sum, r) => sum + (r.cost_usd ?? 0), 0),
      tokens: rows.reduce((sum, r) => sum + tokensOf(r), 0),
    }),
    [rows],
  )

  const deviceOptions = devicesState.status === 'success' ? devicesState.data.devices : []
  const deviceNames = useMemo(() => new Map(deviceOptions.map((d) => [d.id, d.name])), [deviceOptions])

  const matrix = useMemo(
    () => (mixState.status === 'success' ? buildMatrix(mixState.data, deviceNames) : null),
    [mixState, deviceNames],
  )
  const mixSegments: CompositionSegment[] = useMemo(
    () => (matrix?.devices ?? []).map((d) => ({ key: d.id, label: d.name, color: colorForSlot(d.colorIndex) })),
    [matrix],
  )
  const mixRows = useMemo(
    () => (matrix?.sources ?? []).filter((s) => s.tokens > 0).map((s) => ({ key: s.key, label: labelForKey(s.key) })),
    [matrix],
  )

  const summaryError = summaryState.status === 'error' ? summaryState.message : null

  return (
    <div>
      <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
        <h1>에이전트</h1>
      </div>

      <div className="mb-4 flex flex-wrap items-end gap-3 max-phone:flex-col max-phone:items-stretch">
        <RowField label="기기">
          <Select
            id="agent-device-filter"
            value={device}
            onValueChange={(v) => setDevice(v)}
            options={[{ value: '', label: '전체 기기' }, ...deviceOptions.map((d) => ({ value: d.id, label: d.name }))]}
          />
        </RowField>
        {devicesState.status === 'error' && (
          <p className="m-0 text-200 text-danger" role="alert">
            기기 목록을 불러오지 못했습니다: {devicesState.message}{' '}
            <Button touch size="small" appearance="subtle" onClick={devicesState.reload}>
              다시 시도
            </Button>
          </p>
        )}
        {device && (
          <Button touch size="small" appearance="subtle" onClick={() => setDevice('')}>
            필터 해제
          </Button>
        )}
      </div>

      <Card
        className="mb-6"
        title="에이전트 요약"
        subtitle={device ? `${deviceNames.get(device) ?? device} 기기의 사용량만 표시 중` : undefined}
        controls={<SegmentedControl ariaLabel="요약 지표 전환" value={summaryMetric} onChange={setSummaryMetric} options={METRIC_OPTIONS} />}
      >
        <ErrorBoundary label="요약을 표시할 수 없습니다">
          <StateContainer loading={summaryState.status === 'loading'} error={summaryError} onRetry={summaryState.reload}>
            <div className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3">
              {SOURCE_ORDER.map((source) => (
                <AgentSummaryCard
                  key={source}
                  source={source}
                  row={byKey.get(source)}
                  metric={summaryMetric}
                  total={summaryMetric === 'cost' ? totals.cost : totals.tokens}
                />
              ))}
            </div>
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <Card
        className="mb-6"
        title="에이전트별 사용량 추이"
        controls={
          <>
            <SegmentedControl ariaLabel="추이 지표 전환" value={trendMetric} onChange={setTrendMetric} options={METRIC_OPTIONS} />
            {canBucketHourly && (
              <SegmentedControl ariaLabel="추이 단위 전환" value={effectiveBucket} onChange={setBucket} options={BUCKET_OPTIONS} />
            )}
          </>
        }
      >
        <ErrorBoundary label="차트를 표시할 수 없습니다">
          <StateContainer
            loading={trendState.status === 'loading'}
            error={trendState.status === 'error' ? trendState.message : null}
            onRetry={trendState.reload}
            empty={trendState.status === 'success' && trendState.data.series.length === 0}
          >
            {trendState.status === 'success' && trendState.data.series.length > 0 && (
              <TrendChart
                series={trendState.data.series}
                mode={trendMetric}
                colorFor={(key) => SOURCE_COLOR_VARS[key as Source] ?? colorForSlot(0)}
                span={{ range, bucket: trendState.data.bucket }}
              />
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <ThroughputCard
        title="에이전트별 분당 토큰"
        loading={throughputState.status === 'loading'}
        error={throughputState.status === 'error' ? throughputState.message : null}
        onRetry={throughputState.reload}
        data={throughputState.status === 'success' ? throughputState.data : null}
        labelFor={(g) => labelForKey(g.key)}
        keyHeader="에이전트"
      />

      {!device && (
        <Card
          className="mb-6"
          title="에이전트 × 기기"
          subtitle="각 에이전트의 사용량이 어느 기기에서 나왔는지"
          controls={<SegmentedControl ariaLabel="구성 지표 전환" value={mixMetric} onChange={setMixMetric} options={METRIC_OPTIONS} />}
        >
          <ErrorBoundary label="차트를 표시할 수 없습니다">
            <StateContainer
              loading={mixState.status === 'loading'}
              error={mixState.status === 'error' ? mixState.message : null}
              onRetry={mixState.reload}
              empty={mixState.status === 'success' && mixRows.length === 0}
            >
              {matrix && mixRows.length > 0 && (
                <>
                  <CompositionChart
                    rows={mixRows}
                    segments={mixSegments}
                    value={(sourceKey, deviceId) => {
                      const cell = matrix.cell(sourceKey as Source, deviceId)
                      return mixMetric === 'cost' ? cell.cost : cell.tokens
                    }}
                    formatValue={metricFormat(mixMetric)}
                    labelWidth={110}
                  />
                  <CompositionLegend segments={mixSegments} />
                </>
              )}
            </StateContainer>
          </ErrorBoundary>
        </Card>
      )}

      <h2>에이전트별 상세</h2>
      <StateContainer
        loading={summaryState.status === 'loading'}
        error={summaryError}
        onRetry={summaryState.reload}
        empty={summaryState.status === 'success' && rows.length === 0}
        emptyMessage="해당 기간에 사용 내역이 없습니다."
      >
        {summaryState.status === 'success' && rows.length > 0 && (
          <DataTable columns={AGENT_COLUMNS} rows={rows} rowKey={(r) => r.key} initialSortKey="cost" />
        )}
      </StateContainer>
    </div>
  )
}

function AgentSummaryCard({ source, row, metric, total }: { source: Source; row: BreakdownRow | undefined; metric: Metric; total: number }) {
  const color = SOURCE_COLOR_VARS[source]
  const tokens = row ? tokensOf(row) : 0
  const hasData = !!row && (tokens > 0 || row.events > 0)
  const value = metric === 'cost' ? row?.cost_usd ?? null : tokens
  const measured = metric === 'cost' ? row?.cost_usd ?? 0 : tokens
  const share = total > 0 ? measured / total : 0

  return (
    // An agent with no events in the range still gets a card (fixed color slot,
    // fixed order) — dashed and muted, so "no data" never reads as "zero cost".
    <div className={`rounded-lg border border-stroke2 px-4 py-3.5 ${hasData ? 'bg-bg2' : 'border-dashed bg-transparent'}`}>
      <div className="flex items-center gap-0.5 text-200">
        <span className="mr-1.5 inline-block size-2.5 rounded-sm align-[-1px]" style={{ background: color }} />
        <span className="font-semibold text-fg1">{labelForKey(source)}</span>
        {hasData && <span className="ml-auto text-fg3 tabular-nums">{formatPercent(share, share > 0 && share < 0.01 ? 1 : 0)}</span>}
      </div>
      <div className={`mx-0 mt-1 mb-2.5 text-600 leading-600 font-semibold tabular-nums ${hasData ? '' : 'text-fg3'}`} title={metric === 'cost' ? undefined : formatFull(tokens)}>
        {hasData ? (metric === 'cost' ? formatUsd(value as number | null) : formatCompact(tokens)) : '—'}
      </div>
      <div className="h-1 overflow-hidden rounded-md bg-bg6" aria-hidden="true">
        <div className="h-full rounded-md" style={{ width: `${Math.min(100, share * 100)}%`, background: color }} />
      </div>
      <div className="mt-2 text-200 text-fg3">
        {hasData ? (
          <>
            {metric === 'cost' ? (
              <span title={formatFull(tokens)}>토큰 {formatCompact(tokens)}</span>
            ) : (
              <span>비용 {formatUsd(row?.cost_usd ?? null)}</span>
            )}
            {' · '}
            <span>이벤트 {formatFull(row?.events ?? 0)}</span>
            {row?.cost_usd === null && (
              <>
                {' '}
                <Badge kind="unknown" />
              </>
            )}
          </>
        ) : (
          '이 기간에 사용 내역 없음'
        )}
      </div>
    </div>
  )
}

const AGENT_COLUMNS: Column<BreakdownRow>[] = [
  {
    key: 'agent',
    header: '에이전트',
    media: (r) => <Swatch color={SOURCE_COLOR_VARS[r.key as Source] ?? 'var(--text-muted)'} />,
    render: (r) => labelForKey(r.key),
    sortValue: (r) => labelForKey(r.key),
  },
  costColumn('cost', '비용', (r) => r.cost_usd, { badge: true, ratio: (r) => r.cost_known_ratio }),
  tokenColumn('input', '입력', (r) => r.input),
  tokenColumn('output', '출력', (r) => r.output),
  tokenColumn('reasoning', '사고', (r) => r.reasoning),
  tokenColumn('cache_read', '캐시 읽기', (r) => r.cache_read),
  tokenColumn('cache_write', '캐시 쓰기', (r) => r.cache_write),
  { key: 'cache_hit_ratio', header: '캐시 적중률', align: 'right', render: (r) => formatPercent(cacheHitRatio(r), 1), sortValue: cacheHitRatio },
  tokenColumn('total_tokens', '토큰 합계', tokensOf),
  countColumn('events', '이벤트', (r) => r.events),
  dateTimeColumn('last_seen', '최근 사용', (r) => r.last_seen),
]
