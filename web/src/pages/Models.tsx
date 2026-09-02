import { useEffect, useMemo, useState } from 'react'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { TooltipProps } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { getBreakdown, getTimeseries } from '../api/client'
import type { BreakdownRow, Source, TimeseriesSeries } from '../api/types'
import { Badge } from '../components/Badge'
import { ChartTooltip } from '../components/ChartTooltip'
import { type Column, DataTable } from '../components/DataTable'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { StateContainer } from '../components/StateContainer'
import { costOfPoint, formatBucketLabel, mergeSeries, tokensOfPoint } from '../lib/chartData'
import { colorForKey, labelForKey, TOKEN_CLASS_COLORS, TOKEN_CLASS_LABELS, TOKEN_CLASS_ORDER } from '../lib/colors'
import { formatCompact, formatFull, formatPercent, formatUsd } from '../lib/format'
import { useApiData } from '../hooks/useApiData'
import { useFilters } from '../state/FiltersContext'

const SOURCES: Source[] = ['claude_code', 'codex', 'antigravity']

/** Top-N cutoff for both bar charts when no wider "top N selector" exists yet — see "전체 보기". */
const TOP_N = 15
const TOP_TIMESERIES_MODELS = 5

type MetricMode = 'cost' | 'tokens' | 'both'
const METRIC_STORAGE_KEY = 'aum_models_metric'

function loadMetric(): MetricMode {
  try {
    const v = localStorage.getItem(METRIC_STORAGE_KEY)
    if (v === 'cost' || v === 'tokens' || v === 'both') return v
  } catch {
    // localStorage unavailable (private mode, disabled storage, etc.) — fall through to default.
  }
  return 'both'
}

function saveMetric(mode: MetricMode): void {
  try {
    localStorage.setItem(METRIC_STORAGE_KEY, mode)
  } catch {
    // ignore — non-persistent is an acceptable degradation
  }
}

function totalTokens(r: BreakdownRow): number {
  return r.input + r.output + r.cache_read + r.cache_write
}

function cacheHitRatio(r: BreakdownRow): number {
  const denom = r.input + r.cache_read
  return denom > 0 ? r.cache_read / denom : 0
}

export function Models() {
  const { range, refreshKey } = useFilters()
  const [source, setSource] = useState<Source | ''>('')
  const [metric, setMetric] = useState<MetricMode>(loadMetric)
  const [showAll, setShowAll] = useState(false)
  const [tsMode, setTsMode] = useState<'cost' | 'tokens'>('tokens')

  useEffect(() => saveMetric(metric), [metric])

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
  const tokenRows = useMemo(() => [...allRows].sort((a, b) => totalTokens(b) - totalTokens(a)), [allRows])
  const grandTotalTokens = useMemo(() => allRows.reduce((sum, r) => sum + totalTokens(r), 0), [allRows])

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

      <div className="panel-title-row">
        <div className="chart-toggle" role="group" aria-label="지표 전환">
          <button type="button" className={`chip${metric === 'cost' ? ' chip-active' : ''}`} onClick={() => setMetric('cost')}>
            비용
          </button>
          <button type="button" className={`chip${metric === 'tokens' ? ' chip-active' : ''}`} onClick={() => setMetric('tokens')}>
            토큰
          </button>
          <button type="button" className={`chip${metric === 'both' ? ' chip-active' : ''}`} onClick={() => setMetric('both')}>
            둘 다
          </button>
        </div>
        {canExpand && (
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setShowAll((v) => !v)}>
            {showAll ? `상위 ${TOP_N}개만 보기` : `전체 보기 (${allRows.length}개)`}
          </button>
        )}
      </div>

      <div className={`grid${metric === 'both' ? ' grid-cols-2' : ''}`}>
        {(metric === 'cost' || metric === 'both') && (
          <div className="chart-card">
            <h2>모델별 비용{costTruncated ? ` (상위 ${TOP_N}개)` : ''}</h2>
            <StateContainer
              loading={state.status === 'loading'}
              error={state.status === 'error' ? state.message : null}
              empty={state.status === 'success' && visibleCostRows.length === 0}
              emptyMessage="가격이 알려진 모델 사용 내역이 없습니다."
            >
              {visibleCostRows.length > 0 && <ModelCostBarChart rows={visibleCostRows} />}
            </StateContainer>
          </div>
        )}
        {(metric === 'tokens' || metric === 'both') && (
          <div className="chart-card">
            <h2>모델별 토큰 사용량{tokenTruncated ? ` (상위 ${TOP_N}개)` : ''}</h2>
            <StateContainer
              loading={state.status === 'loading'}
              error={state.status === 'error' ? state.message : null}
              empty={state.status === 'success' && visibleTokenRows.length === 0}
              emptyMessage="해당 기간에 데이터가 없습니다."
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

      <div className="chart-card">
        <div className="panel-title-row">
          <h2>모델별 사용량 추이 (상위 {TOP_TIMESERIES_MODELS}개)</h2>
          <div className="chart-toggle" role="group" aria-label="추이 차트 값 전환">
            <button type="button" className={`chip${tsMode === 'cost' ? ' chip-active' : ''}`} onClick={() => setTsMode('cost')}>
              비용
            </button>
            <button type="button" className={`chip${tsMode === 'tokens' ? ' chip-active' : ''}`} onClick={() => setTsMode('tokens')}>
              토큰
            </button>
          </div>
        </div>
        <ErrorBoundary label="차트를 표시할 수 없습니다">
          <StateContainer
            loading={timeseriesState.status === 'loading'}
            error={timeseriesState.status === 'error' ? timeseriesState.message : null}
            onRetry={timeseriesState.reload}
            empty={timeseriesState.status === 'success' && timeseriesSeries.length === 0}
          >
            {timeseriesState.status === 'success' && timeseriesSeries.length > 0 && (
              <ModelTimeseriesChart series={timeseriesSeries} mode={tsMode} />
            )}
          </StateContainer>
        </ErrorBoundary>
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
        <Bar dataKey="cost" radius={[0, 4, 4, 0]} maxBarSize={18} fill="var(--series-1)" />
      </BarChart>
    </ResponsiveContainer>
  )
}

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
  const data: TokenDatum[] = rows.map((r) => {
    const total = totalTokens(r)
    return {
      name: r.key,
      input: r.input,
      cache_read: r.cache_read,
      cache_write: r.cache_write,
      output: r.output,
      reasoning: r.reasoning,
      total,
      share: grandTotal > 0 ? total / grandTotal : 0,
    }
  })
  return (
    <ResponsiveContainer width="100%" height={Math.max(180, data.length * 34)}>
      <BarChart data={data} layout="vertical" margin={{ left: 8, right: 16 }}>
        <CartesianGrid stroke="var(--border)" horizontal={false} />
        <XAxis type="number" tickFormatter={(v: number) => formatCompact(v)} tick={{ fill: 'var(--text-muted)', fontSize: 11 }} axisLine={false} tickLine={false} />
        <YAxis type="category" dataKey="name" tick={{ fill: 'var(--text-secondary)', fontSize: 12 }} axisLine={false} tickLine={false} width={140} />
        <Tooltip content={<TokenBarTooltip />} cursor={{ fill: 'var(--border)', opacity: 0.4 }} />
        {TOKEN_CLASS_ORDER.map((cls) => (
          <Bar key={cls} dataKey={cls} stackId="tokens" fill={TOKEN_CLASS_COLORS[cls]} radius={[2, 2, 2, 2]} maxBarSize={18} />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}

function TokenClassLegend() {
  return (
    <div className="legend-row">
      {TOKEN_CLASS_ORDER.map((cls) => (
        <span key={cls}>
          <span className="legend-swatch" style={{ background: TOKEN_CLASS_COLORS[cls] }} />
          {TOKEN_CLASS_LABELS[cls]}
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
    <div
      style={{
        background: 'var(--surface-raised)',
        border: '1px solid var(--border)',
        borderRadius: 6,
        padding: '8px 10px',
        fontSize: 12.5,
        boxShadow: 'var(--shadow)',
        minWidth: 190,
      }}
    >
      <div style={{ color: 'var(--text-muted)', marginBottom: 4 }}>{label}</div>
      {TOKEN_CLASS_ORDER.map((cls) => (
        <div key={cls} style={{ display: 'flex', gap: 8, justifyContent: 'space-between', color: 'var(--text-primary)' }}>
          <span>
            <span
              style={{
                display: 'inline-block',
                width: 8,
                height: 8,
                borderRadius: 2,
                background: TOKEN_CLASS_COLORS[cls],
                marginRight: 6,
              }}
            />
            {TOKEN_CLASS_LABELS[cls]}
          </span>
          <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatFull(d[cls])}</span>
        </div>
      ))}
      <div style={{ borderTop: '1px solid var(--border)', margin: '6px 0 4px' }} />
      <div style={{ display: 'flex', gap: 8, justifyContent: 'space-between', color: 'var(--text-primary)', fontWeight: 600 }}>
        <span>합계</span>
        <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatFull(d.total)}</span>
      </div>
      <div style={{ display: 'flex', gap: 8, justifyContent: 'space-between', color: 'var(--text-secondary)' }}>
        <span>전체 대비 비중</span>
        <span style={{ fontVariantNumeric: 'tabular-nums' }}>{formatPercent(d.share, 1)}</span>
      </div>
      {d.reasoning > 0 && (
        <div style={{ marginTop: 4, color: 'var(--text-muted)', fontSize: 11.5 }}>출력 중 사고 {formatFull(d.reasoning)}</div>
      )}
    </div>
  )
}

function ModelTimeseriesChart({ series, mode }: { series: TimeseriesSeries[]; mode: 'cost' | 'tokens' }) {
  const { data, keys } = mergeSeries(series, mode === 'cost' ? costOfPoint : tokensOfPoint)
  if (data.length === 0) return null
  return (
    <ResponsiveContainer width="100%" height={280}>
      <AreaChart data={data} margin={{ left: 4, right: 4 }}>
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
        <Tooltip content={<ChartTooltip formatValue={mode === 'cost' ? formatUsd : formatCompact} />} cursor={{ stroke: 'var(--border-strong)', strokeWidth: 1 }} />
        <Legend formatter={(value: string) => <span style={{ color: 'var(--text-secondary)', fontSize: 12 }}>{labelForKey(value)}</span>} />
        {keys.map((key, i) => (
          <Area
            key={key}
            type="monotone"
            dataKey={key}
            stackId="usage"
            stroke={colorForKey(key, i)}
            fill={colorForKey(key, i)}
            fillOpacity={0.5}
            strokeWidth={2}
          />
        ))}
      </AreaChart>
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
      render: (r) => <span title={formatFull(totalTokens(r))}>{formatCompact(totalTokens(r))}</span>,
      sortValue: (r) => totalTokens(r),
    },
    { key: 'events', header: '이벤트', align: 'right', render: (r) => formatFull(r.events), sortValue: (r) => r.events },
  ]
  return rows.length === 0 ? (
    <p className="text-muted">해당 기간에 데이터가 없습니다.</p>
  ) : (
    <DataTable columns={columns} rows={rows} rowKey={(r) => `${r.source}:${r.key}`} initialSortKey="cost" />
  )
}
