import { useMemo, useState } from 'react'
import { getBreakdown, getDevices, getTimeseries } from '../api/client'
import type { BreakdownRow, Source, TimeseriesBucket } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { CompositionChart, CompositionLegend, type CompositionSegment } from '../components/CompositionChart'
import { type Column, DataTable } from '../components/DataTable'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SegmentedControl } from '../components/SegmentedControl'
import { StateContainer } from '../components/StateContainer'
import { TrendChart } from '../components/TrendChart'
import { useApiData } from '../hooks/useApiData'
import { SOURCE_COLOR_VARS, SOURCE_ORDER, colorForSlot, labelForKey } from '../lib/colors'
import { rangeDays } from '../lib/dateRange'
import { formatCompact, formatDateTime, formatFull, formatPercent, formatUsd } from '../lib/format'
import { buildMatrix, fetchSourceDeviceMatrix, tokensOf } from '../lib/matrix'
import { useFilters } from '../state/FiltersContext'

type Metric = 'cost' | 'tokens'

/** Longest range that still makes sense as hourly bars. */
const HOURLY_MAX_DAYS = 7

const METRIC_OPTIONS = [
  { value: 'cost' as const, label: '비용' },
  { value: 'tokens' as const, label: '토큰' },
]

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

  // An hourly bucket over a month is ~720 bars — unreadable and a large
  // payload — so it is only offered on short ranges, and a range change that
  // outgrows it silently falls back to daily.
  const hourlyAllowed = rangeDays(range.from, range.to) <= HOURLY_MAX_DAYS
  const effectiveBucket: TimeseriesBucket = hourlyAllowed ? bucket : 'day'

  const devicesState = useApiData(() => getDevices(), [refreshKey])
  const summaryState = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'source', device: device || undefined, limit: 50 }),
    [range.from, range.to, device, refreshKey],
  )
  const trendState = useApiData(
    () => getTimeseries({ from: range.from, to: range.to, bucket: effectiveBucket, group_by: 'source', device: device || undefined }),
    [range.from, range.to, effectiveBucket, device, refreshKey],
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

  return (
    <div>
      <div className="page-header">
        <h1>에이전트</h1>
      </div>

      <div className="filters-row">
        <div className="field">
          <label htmlFor="agent-device-filter">기기</label>
          <select id="agent-device-filter" value={device} onChange={(e) => setDevice(e.target.value)}>
            <option value="">전체 기기</option>
            {deviceOptions.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </div>
        {device && (
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setDevice('')}>
            필터 해제
          </button>
        )}
      </div>

      <Card
        className="section"
        title="에이전트 요약"
        subtitle={device ? `${deviceNames.get(device) ?? device} 기기의 사용량만 표시 중` : undefined}
        controls={<SegmentedControl ariaLabel="요약 지표 전환" value={summaryMetric} onChange={setSummaryMetric} options={METRIC_OPTIONS} />}
      >
        <ErrorBoundary label="요약을 표시할 수 없습니다">
          <StateContainer
            loading={summaryState.status === 'loading'}
            error={summaryState.status === 'error' ? summaryState.message : null}
            onRetry={summaryState.reload}
          >
            <div className="agent-cards">
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
        className="section"
        title="에이전트별 사용량 추이"
        controls={
          <>
            <SegmentedControl ariaLabel="추이 지표 전환" value={trendMetric} onChange={setTrendMetric} options={METRIC_OPTIONS} />
            <SegmentedControl
              ariaLabel="추이 단위 전환"
              value={effectiveBucket}
              onChange={setBucket}
              options={[
                { value: 'day', label: '일별' },
                {
                  value: 'hour',
                  label: '시간별',
                  disabled: !hourlyAllowed,
                  title: hourlyAllowed ? undefined : `시간별 보기는 기간이 ${HOURLY_MAX_DAYS}일 이하일 때만 쓸 수 있습니다.`,
                },
              ]}
            />
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
              />
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      {!device && (
        <Card
          className="section"
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
                    formatValue={mixMetric === 'cost' ? formatUsd : formatCompact}
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
        error={summaryState.status === 'error' ? summaryState.message : null}
        onRetry={summaryState.reload}
        empty={summaryState.status === 'success' && rows.length === 0}
        emptyMessage="해당 기간에 사용 내역이 없습니다."
      >
        {summaryState.status === 'success' && rows.length > 0 && <AgentsTable rows={rows} />}
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
    <div className={`agent-card${hasData ? '' : ' agent-card-empty'}`}>
      <div className="agent-card-head">
        <span className="legend-swatch" style={{ background: color }} />
        <span className="agent-card-name">{labelForKey(source)}</span>
        {hasData && <span className="agent-card-share">{formatPercent(share, share > 0 && share < 0.01 ? 1 : 0)}</span>}
      </div>
      <div
        className="agent-card-value"
        title={metric === 'cost' ? undefined : formatFull(tokens)}
      >
        {hasData ? (metric === 'cost' ? formatUsd(value as number | null) : formatCompact(tokens)) : '—'}
      </div>
      <div className="share-track" aria-hidden="true">
        <div className="share-fill" style={{ width: `${Math.min(100, share * 100)}%`, background: color }} />
      </div>
      <div className="agent-card-meta">
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

function cacheHitRatio(r: BreakdownRow): number {
  const denom = r.input + r.cache_read
  return denom > 0 ? r.cache_read / denom : 0
}

function AgentsTable({ rows }: { rows: BreakdownRow[] }) {
  const columns: Column<BreakdownRow>[] = [
    {
      key: 'agent',
      header: '에이전트',
      render: (r) => (
        <span>
          <span className="legend-swatch" style={{ background: SOURCE_COLOR_VARS[r.key as Source] ?? 'var(--text-muted)' }} />
          {labelForKey(r.key)}
        </span>
      ),
      sortValue: (r) => labelForKey(r.key),
    },
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
    { key: 'input', header: '입력', align: 'right', render: (r) => <span title={formatFull(r.input)}>{formatCompact(r.input)}</span>, sortValue: (r) => r.input },
    { key: 'output', header: '출력', align: 'right', render: (r) => <span title={formatFull(r.output)}>{formatCompact(r.output)}</span>, sortValue: (r) => r.output },
    {
      key: 'reasoning',
      header: '사고',
      align: 'right',
      render: (r) => <span title={formatFull(r.reasoning)}>{formatCompact(r.reasoning)}</span>,
      sortValue: (r) => r.reasoning,
    },
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
    {
      key: 'cache_hit_ratio',
      header: '캐시 적중률',
      align: 'right',
      render: (r) => formatPercent(cacheHitRatio(r), 1),
      sortValue: (r) => cacheHitRatio(r),
    },
    {
      key: 'total_tokens',
      header: '토큰 합계',
      align: 'right',
      render: (r) => <span title={formatFull(tokensOf(r))}>{formatCompact(tokensOf(r))}</span>,
      sortValue: (r) => tokensOf(r),
    },
    { key: 'events', header: '이벤트', align: 'right', render: (r) => formatFull(r.events), sortValue: (r) => r.events },
    { key: 'last_seen', header: '최근 사용', render: (r) => formatDateTime(r.last_seen), sortValue: (r) => r.last_seen },
  ]
  return <DataTable columns={columns} rows={rows} rowKey={(r) => r.key} initialSortKey="cost" />
}
