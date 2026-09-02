import { useEffect, useMemo, useState } from 'react'
import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { getBreakdown, getTimeseries } from '../api/client'
import type { BreakdownRow, Source } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { ChartTooltip } from '../components/ChartTooltip'
import {
  CompositionChart,
  type CompositionRow,
  type CompositionSegment,
  TooltipDivider,
  TooltipRow,
  TooltipShell,
} from '../components/CompositionChart'
import { type Column, DataTable } from '../components/DataTable'
import { costColumn, countColumn, tokenColumn } from '../components/columns'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SegmentedControl } from '../components/SegmentedControl'
import { StateContainer } from '../components/StateContainer'
import { TrendChart } from '../components/TrendChart'
import { type Metric, METRIC_OPTIONS } from '../lib/chartData'
import {
  SOURCE_ORDER,
  TOKEN_CLASS_COLORS,
  TOKEN_CLASS_LABELS,
  TOKEN_CLASS_ORDER,
  type TokenClass,
  colorForKey,
  labelForKey,
} from '../lib/colors'
import { formatCompact, formatFull, formatPercent, formatUsd } from '../lib/format'
import { readStored, writeStored } from '../lib/storage'
import { cacheHitRatio, tokensOf } from '../lib/usage'
import { useApiData } from '../hooks/useApiData'
import { useFilters } from '../state/FiltersContext'

/** Top-N cutoff for both bar charts when no wider "top N selector" exists yet — see "전체 보기". */
const TOP_N = 15
const TOP_TIMESERIES_MODELS = 5

/** The bar charts add "둘 다" to the usual 비용/토큰 pair — side-by-side is the
 * default because cost and token volume rank models very differently. */
type MetricMode = Metric | 'both'
const METRIC_MODE_OPTIONS = [...METRIC_OPTIONS, { value: 'both' as const, label: '둘 다' }]
const METRIC_STORAGE_KEY = 'aum_models_metric'

function loadMetric(): MetricMode {
  const v = readStored(METRIC_STORAGE_KEY)
  return v === 'cost' || v === 'tokens' || v === 'both' ? v : 'both'
}

/** One horizontal bar per model, tall enough to stay legible as rows are added. */
const barHeight = (rowCount: number) => Math.max(180, rowCount * 34)

const COST_SEGMENT: CompositionSegment[] = [{ key: 'cost', label: '비용', color: 'var(--series-1)' }]
const TOKEN_SEGMENTS: CompositionSegment[] = TOKEN_CLASS_ORDER.map((cls) => ({
  key: cls,
  label: TOKEN_CLASS_LABELS[cls],
  color: TOKEN_CLASS_COLORS[cls],
}))

export function Models() {
  const { range, refreshKey } = useFilters()
  const [source, setSource] = useState<Source | ''>('')
  const [metric, setMetric] = useState<MetricMode>(loadMetric)
  const [showAll, setShowAll] = useState(false)
  const [tsMode, setTsMode] = useState<Metric>('tokens')

  useEffect(() => writeStored(METRIC_STORAGE_KEY, metric), [metric])

  const state = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'model', source: source || undefined, limit: 100 }),
    [range.from, range.to, source, refreshKey],
  )
  const timeseriesState = useApiData(
    () => getTimeseries({ from: range.from, to: range.to, bucket: 'day', group_by: 'model', source: source || undefined }),
    [range.from, range.to, source, refreshKey],
  )

  const allRows = state.status === 'success' ? state.data.rows : []

  const costRows = useMemo(
    () => [...allRows].filter((r) => r.cost_usd !== null).sort((a, b) => (b.cost_usd ?? 0) - (a.cost_usd ?? 0)),
    [allRows],
  )
  const tokenRows = useMemo(() => [...allRows].sort((a, b) => tokensOf(b) - tokensOf(a)), [allRows])
  const grandTotalTokens = useMemo(() => allRows.reduce((sum, r) => sum + tokensOf(r), 0), [allRows])

  const canExpand = costRows.length > TOP_N || tokenRows.length > TOP_N
  const visibleCostRows = showAll ? costRows : costRows.slice(0, TOP_N)
  const visibleTokenRows = showAll ? tokenRows : tokenRows.slice(0, TOP_N)
  const costTruncated = !showAll && costRows.length > TOP_N
  const tokenTruncated = !showAll && tokenRows.length > TOP_N

  // Same model set/order as the token chart's "top by usage" ranking — the
  // per-day trend line is only useful for the handful of models that
  // actually dominate the range.
  const topModelKeys = useMemo(() => tokenRows.slice(0, TOP_TIMESERIES_MODELS).map((r) => r.key), [tokenRows])
  const timeseriesSeries = useMemo(() => {
    if (timeseriesState.status !== 'success') return []
    const rank = new Map(topModelKeys.map((k, i) => [k, i]))
    return timeseriesState.data.series.filter((s) => rank.has(s.key)).sort((a, b) => rank.get(a.key)! - rank.get(b.key)!)
  }, [timeseriesState, topModelKeys])

  const stateError = state.status === 'error' ? state.message : null

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
            {SOURCE_ORDER.map((s) => (
              <option key={s} value={s}>
                {labelForKey(s)}
              </option>
            ))}
          </select>
        </div>
      </div>

      <Card
        className="section"
        title="모델별 비용·토큰"
        controls={
          <>
            <SegmentedControl ariaLabel="지표 전환" value={metric} onChange={setMetric} options={METRIC_MODE_OPTIONS} />
            {canExpand && (
              <button type="button" className="btn btn-sm btn-ghost" onClick={() => setShowAll((v) => !v)}>
                {showAll ? `상위 ${TOP_N}개만 보기` : `전체 보기 (${allRows.length}개)`}
              </button>
            )}
          </>
        }
      >
        <div className={`grid${metric === 'both' ? ' grid-cols-2' : ''}`}>
          {(metric === 'cost' || metric === 'both') && (
            <div>
              <h3>모델별 비용{costTruncated ? ` (상위 ${TOP_N}개)` : ''}</h3>
              <StateContainer
                loading={state.status === 'loading'}
                error={stateError}
                empty={state.status === 'success' && visibleCostRows.length === 0}
                emptyMessage="가격이 알려진 모델 사용 내역이 없습니다."
              >
                {visibleCostRows.length > 0 && <ModelCostBarChart rows={visibleCostRows} />}
              </StateContainer>
            </div>
          )}
          {(metric === 'tokens' || metric === 'both') && (
            <div>
              <h3>모델별 토큰 사용량{tokenTruncated ? ` (상위 ${TOP_N}개)` : ''}</h3>
              <StateContainer
                loading={state.status === 'loading'}
                error={stateError}
                empty={state.status === 'success' && visibleTokenRows.length === 0}
              >
                {visibleTokenRows.length > 0 && (
                  <>
                    <ModelTokenBarChart rows={visibleTokenRows} grandTotal={grandTotalTokens} />
                    <TokenClassLegend />
                  </>
                )}
              </StateContainer>
            </div>
          )}
        </div>
      </Card>

      <Card
        className="section"
        title={`모델별 사용량 추이 (상위 ${TOP_TIMESERIES_MODELS}개)`}
        controls={<SegmentedControl ariaLabel="추이 차트 값 전환" value={tsMode} onChange={setTsMode} options={METRIC_OPTIONS} />}
      >
        <ErrorBoundary label="차트를 표시할 수 없습니다">
          <StateContainer
            loading={timeseriesState.status === 'loading'}
            error={timeseriesState.status === 'error' ? timeseriesState.message : null}
            onRetry={timeseriesState.reload}
            empty={timeseriesState.status === 'success' && timeseriesSeries.length === 0}
          >
            {timeseriesState.status === 'success' && timeseriesSeries.length > 0 && (
              <TrendChart series={timeseriesSeries} mode={tsMode} colorFor={colorForKey} variant="area" height={280} />
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <h2>전체 모델</h2>
      <StateContainer
        loading={state.status === 'loading'}
        error={stateError}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.rows.length === 0}
      >
        {state.status === 'success' && <ModelsTable rows={state.data.rows} />}
      </StateContainer>
    </div>
  )
}

function ModelCostBarChart({ rows }: { rows: BreakdownRow[] }) {
  const costByKey = new Map(rows.map((r) => [r.key, r.cost_usd ?? 0]))
  return (
    <CompositionChart
      rows={rows.map((r) => ({ key: r.key, label: r.key }))}
      segments={COST_SEGMENT}
      value={(rowKey) => costByKey.get(rowKey) ?? 0}
      formatValue={formatUsd}
      height={barHeight(rows.length)}
      maxBarSize={18}
      barRadius={[0, 4, 4, 0]}
      barStroke={false}
      tooltip={<ChartTooltip formatValue={formatUsd} />}
    />
  )
}

/** Datum shape the token tooltip reads: the four stacked classes plus the
 * `reasoning` subset and the model's share of the whole range. */
interface TokenDatum {
  name: string
  input: number
  cache_read: number
  cache_write: number
  output: number
  reasoning: number
  total: number
  share: number
}

function ModelTokenBarChart({ rows, grandTotal }: { rows: BreakdownRow[]; grandTotal: number }) {
  const byKey = new Map(rows.map((r) => [r.key, r]))
  const compositionRows: CompositionRow[] = rows.map((r) => ({
    key: r.key,
    label: r.key,
    extra: { reasoning: r.reasoning, share: grandTotal > 0 ? tokensOf(r) / grandTotal : 0 },
  }))
  return (
    <CompositionChart
      rows={compositionRows}
      segments={TOKEN_SEGMENTS}
      value={(rowKey, segmentKey) => byKey.get(rowKey)?.[segmentKey as TokenClass] ?? 0}
      formatValue={formatCompact}
      height={barHeight(rows.length)}
      maxBarSize={18}
      barStroke={false}
      tooltip={<TokenBarTooltip />}
    />
  )
}

function TokenClassLegend() {
  return (
    <div className="legend-row">
      {TOKEN_SEGMENTS.map((seg) => (
        <span key={seg.key}>
          <span className="legend-swatch" style={{ background: seg.color }} />
          {seg.label}
        </span>
      ))}
    </div>
  )
}

/** Custom tooltip for the token chart: exact per-class counts + total/share + a reasoning subset line. */
function TokenBarTooltip({ active, payload, label }: TooltipProps<ValueType, NameType>) {
  if (!active || !payload || payload.length === 0) return null
  const d = payload[0].payload as TokenDatum
  return (
    <TooltipShell label={label}>
      {TOKEN_CLASS_ORDER.map((cls) => (
        <TooltipRow key={cls} color={TOKEN_CLASS_COLORS[cls]} label={TOKEN_CLASS_LABELS[cls]}>
          {formatFull(d[cls])}
        </TooltipRow>
      ))}
      <TooltipDivider />
      <TooltipRow label="합계" bold>
        {formatFull(d.total)}
      </TooltipRow>
      <TooltipRow label="전체 대비 비중" muted>
        {formatPercent(d.share, 1)}
      </TooltipRow>
      {d.reasoning > 0 && (
        <div style={{ marginTop: 4, color: 'var(--text-muted)', fontSize: 11.5 }}>출력 중 사고 {formatFull(d.reasoning)}</div>
      )}
    </TooltipShell>
  )
}

const MODEL_COLUMNS: Column<BreakdownRow>[] = [
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
  costColumn('cost', '비용', (r) => r.cost_usd),
  tokenColumn('input', '입력', (r) => r.input),
  tokenColumn('output', '출력', (r) => r.output),
  tokenColumn('reasoning', '사고', (r) => r.reasoning),
  tokenColumn('cache_read', '캐시 읽기', (r) => r.cache_read),
  tokenColumn('cache_write', '캐시 쓰기', (r) => r.cache_write),
  { key: 'cache_hit_ratio', header: '캐시 적중률', align: 'right', render: (r) => formatPercent(cacheHitRatio(r), 1), sortValue: cacheHitRatio },
  tokenColumn('total_tokens', '토큰 합계', tokensOf),
  countColumn('events', '이벤트', (r) => r.events),
]

function ModelsTable({ rows }: { rows: BreakdownRow[] }) {
  return rows.length === 0 ? (
    <p className="text-muted">해당 기간에 데이터가 없습니다.</p>
  ) : (
    <DataTable columns={MODEL_COLUMNS} rows={rows} rowKey={(r) => `${r.source}:${r.key}`} initialSortKey="cost" />
  )
}
