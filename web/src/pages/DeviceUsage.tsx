import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { getBreakdown, getDevices, getTimeseries } from '../api/client'
import type { BreakdownRow, Device, Source, TimeseriesBucket, TimeseriesSeries } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { CompositionChart, CompositionLegend, type CompositionSegment } from '../components/CompositionChart'
import { type Column, DataTable } from '../components/DataTable'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { SegmentedControl } from '../components/SegmentedControl'
import { StateContainer } from '../components/StateContainer'
import { StatTile } from '../components/StatTile'
import { TrendChart } from '../components/TrendChart'
import { useApiData } from '../hooks/useApiData'
import { tokensOfPoint } from '../lib/chartData'
import { SOURCE_COLOR_VARS, SOURCE_ORDER, colorForSlot, labelForKey } from '../lib/colors'
import { rangeDays } from '../lib/dateRange'
import { formatCompact, formatDateTime, formatFull, formatRelative, formatUsd, isStale } from '../lib/format'
import { buildMatrix, fetchSourceDeviceMatrix, tokensOf } from '../lib/matrix'
import { useFilters } from '../state/FiltersContext'

type Metric = 'cost' | 'tokens'

/** Longest range that still makes sense as hourly bars. */
const HOURLY_MAX_DAYS = 7

const METRIC_OPTIONS = [
  { value: 'cost' as const, label: '비용' },
  { value: 'tokens' as const, label: '토큰' },
]

/** Past this the trend chart's stacked bands stop being readable, so only the
 * busiest devices get a series. */
const MAX_TREND_DEVICES = 8

interface DeviceUsageRow {
  id: string
  name: string
  usage?: BreakdownRow
  meta?: Device
}

/**
 * Per-device view: which machines the usage came from. Registered devices with
 * no events in the range are still listed (with zeros) so an idle or broken
 * agent is visible instead of silently missing. Device administration lives on
 * the sibling 관리 tab.
 */
export function DeviceUsage() {
  const { range, refreshKey } = useFilters()
  const [source, setSource] = useState<Source | ''>('')
  const [mixMetric, setMixMetric] = useState<Metric>('cost')
  const [trendMetric, setTrendMetric] = useState<Metric>('cost')
  const [bucket, setBucket] = useState<TimeseriesBucket>('day')

  // An hourly bucket over a month is ~720 bars — unreadable and a large
  // payload — so it is only offered on short ranges, and a range change that
  // outgrows it silently falls back to daily.
  const hourlyAllowed = rangeDays(range.from, range.to) <= HOURLY_MAX_DAYS
  const effectiveBucket: TimeseriesBucket = hourlyAllowed ? bucket : 'day'

  const devicesState = useApiData(() => getDevices(), [refreshKey])
  const usageState = useApiData(
    () => getBreakdown({ from: range.from, to: range.to, group_by: 'device', source: source || undefined, limit: 200 }),
    [range.from, range.to, source, refreshKey],
  )
  const trendState = useApiData(
    () => getTimeseries({ from: range.from, to: range.to, bucket: effectiveBucket, group_by: 'device', source: source || undefined }),
    [range.from, range.to, effectiveBucket, source, refreshKey],
  )
  const mixState = useApiData(
    () => fetchSourceDeviceMatrix({ from: range.from, to: range.to, sources: source ? [source] : SOURCE_ORDER }),
    [range.from, range.to, source, refreshKey],
  )

  const devices = devicesState.status === 'success' ? devicesState.data.devices : []
  const usageRows = usageState.status === 'success' ? usageState.data.rows : []

  const deviceNames = useMemo(() => {
    const m = new Map<string, string>()
    for (const d of devices) m.set(d.id, d.name)
    for (const r of usageRows) if (r.name) m.set(r.key, r.name)
    return m
  }, [devices, usageRows])

  // One fixed color slot per device id, taken from the id-sorted union of
  // registered and reporting devices so a source filter never repaints them.
  const colorIndexById = useMemo(() => {
    const ids = [...new Set([...devices.map((d) => d.id), ...usageRows.map((r) => r.key)])].sort()
    return new Map(ids.map((id, i) => [id, i]))
  }, [devices, usageRows])

  const nameOf = (id: string) => deviceNames.get(id) ?? id
  const colorOf = (id: string) => colorForSlot(colorIndexById.get(id) ?? 0)

  const rows: DeviceUsageRow[] = useMemo(() => {
    const usageById = new Map(usageRows.map((r) => [r.key, r]))
    const ids = [...new Set([...devices.filter((d) => !d.revoked_at || usageById.has(d.id)).map((d) => d.id), ...usageById.keys()])]
    const metaById = new Map(devices.map((d) => [d.id, d]))
    return ids.map((id) => ({ id, name: nameOf(id), usage: usageById.get(id), meta: metaById.get(id) }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [devices, usageRows, deviceNames])

  // Revoked devices are no longer collecting, so they are not part of the
  // "N대 중" denominator.
  const registeredCount = devices.filter((d) => !d.revoked_at).length

  const totals = useMemo(
    () => ({
      cost: usageRows.reduce((sum, r) => sum + (r.cost_usd ?? 0), 0),
      tokens: usageRows.reduce((sum, r) => sum + tokensOf(r), 0),
      events: usageRows.reduce((sum, r) => sum + r.events, 0),
      active: usageRows.filter((r) => tokensOf(r) > 0 || r.events > 0).length,
    }),
    [usageRows],
  )

  const matrix = useMemo(
    () => (mixState.status === 'success' ? buildMatrix(mixState.data, deviceNames) : null),
    [mixState, deviceNames],
  )
  const mixSegments: CompositionSegment[] = useMemo(
    () =>
      (source ? [source] : SOURCE_ORDER).map((s) => ({ key: s, label: labelForKey(s), color: SOURCE_COLOR_VARS[s] })),
    [source],
  )
  const mixRows = useMemo(() => (matrix?.devices ?? []).map((d) => ({ key: d.id, label: d.name })), [matrix])

  // Only the busiest devices get a band in the trend chart.
  const trendSeries: TimeseriesSeries[] = useMemo(() => {
    if (trendState.status !== 'success') return []
    const totalOf = (s: TimeseriesSeries) => s.points.reduce((sum, p) => sum + tokensOfPoint(p), 0)
    return [...trendState.data.series].sort((a, b) => totalOf(b) - totalOf(a)).slice(0, MAX_TREND_DEVICES)
  }, [trendState])
  const trendTruncated = trendState.status === 'success' && trendState.data.series.length > trendSeries.length

  return (
    <div>
      <div className="page-header">
        <h1>기기</h1>
      </div>

      <div className="filters-row">
        <div className="field">
          <label htmlFor="device-source-filter">에이전트</label>
          <select id="device-source-filter" value={source} onChange={(e) => setSource(e.target.value as Source | '')}>
            <option value="">전체 에이전트</option>
            {SOURCE_ORDER.map((s) => (
              <option key={s} value={s}>
                {labelForKey(s)}
              </option>
            ))}
          </select>
        </div>
        {source && (
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setSource('')}>
            필터 해제
          </button>
        )}
      </div>

      <ErrorBoundary label="통계 타일을 표시할 수 없습니다">
        <StateContainer
          loading={usageState.status === 'loading'}
          error={usageState.status === 'error' ? usageState.message : null}
          onRetry={usageState.reload}
        >
          {usageState.status === 'success' && (
            <div className="stat-tiles">
              <StatTile
                label="사용 중인 기기"
                value={formatFull(totals.active)}
                hint={registeredCount > 0 ? `등록 ${formatFull(registeredCount)}대 중` : undefined}
              />
              <StatTile label="총 비용" value={formatUsd(totals.cost)} />
              <StatTile label="총 토큰" value={formatCompact(totals.tokens)} title={formatFull(totals.tokens)} />
              <StatTile label="이벤트" value={formatFull(totals.events)} />
            </div>
          )}
        </StateContainer>
      </ErrorBoundary>

      <Card
        className="section"
        title="기기별 에이전트 구성"
        subtitle="각 기기의 사용량을 에이전트별로 나눈 것"
        controls={<SegmentedControl ariaLabel="구성 지표 전환" value={mixMetric} onChange={setMixMetric} options={METRIC_OPTIONS} />}
      >
        <ErrorBoundary label="차트를 표시할 수 없습니다">
          <StateContainer
            loading={mixState.status === 'loading'}
            error={mixState.status === 'error' ? mixState.message : null}
            onRetry={mixState.reload}
            empty={mixState.status === 'success' && mixRows.length === 0}
            emptyMessage="해당 기간에 사용 내역이 있는 기기가 없습니다."
          >
            {matrix && mixRows.length > 0 && (
              <>
                <CompositionChart
                  rows={mixRows}
                  segments={mixSegments}
                  value={(deviceId, sourceKey) => {
                    const cell = matrix.cell(sourceKey as Source, deviceId)
                    return mixMetric === 'cost' ? cell.cost : cell.tokens
                  }}
                  formatValue={mixMetric === 'cost' ? formatUsd : formatCompact}
                />
                <CompositionLegend segments={mixSegments} />
              </>
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <Card
        className="section"
        title="기기별 사용량 추이"
        subtitle={trendTruncated ? `상위 ${MAX_TREND_DEVICES}대만 표시` : undefined}
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
            empty={trendState.status === 'success' && trendSeries.length === 0}
          >
            {trendSeries.length > 0 && (
              <TrendChart
                series={trendSeries}
                mode={trendMetric}
                labelFor={(key) => nameOf(key)}
                colorFor={(key) => colorOf(key)}
              />
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <h2>기기별 상세</h2>
      <StateContainer
        loading={usageState.status === 'loading' || devicesState.status === 'loading'}
        error={usageState.status === 'error' ? usageState.message : devicesState.status === 'error' ? devicesState.message : null}
        onRetry={usageState.reload}
        empty={usageState.status === 'success' && rows.length === 0}
        emptyMessage="등록된 기기가 없습니다."
      >
        {usageState.status === 'success' && rows.length > 0 && <DeviceUsageTable rows={rows} colorOf={colorOf} />}
      </StateContainer>
    </div>
  )
}

function DeviceUsageTable({ rows, colorOf }: { rows: DeviceUsageRow[]; colorOf: (id: string) => string }) {
  const tokens = (r: DeviceUsageRow) => (r.usage ? tokensOf(r.usage) : 0)
  const columns: Column<DeviceUsageRow>[] = [
    {
      key: 'name',
      header: '기기',
      render: (r) => (
        <span>
          <span className="legend-swatch" style={{ background: colorOf(r.id) }} />
          {r.name} {r.meta?.revoked_at && <Badge kind="revoked" />}{' '}
          {r.meta && !r.meta.revoked_at && isStale(r.meta.last_seen) && <Badge kind="stale" />}
          {!r.meta && <Badge kind="unknown">삭제된 기기</Badge>}
        </span>
      ),
      sortValue: (r) => r.name,
    },
    {
      key: 'os',
      header: 'OS / Arch',
      render: (r) => (r.meta ? `${r.meta.os}/${r.meta.arch}` : '-'),
      sortValue: (r) => (r.meta ? `${r.meta.os}/${r.meta.arch}` : ''),
    },
    { key: 'agent_version', header: '에이전트 버전', render: (r) => r.meta?.agent_version ?? '-', sortValue: (r) => r.meta?.agent_version ?? '' },
    {
      key: 'cost',
      header: '비용',
      align: 'right',
      render: (r) => (
        <span>
          {r.usage ? formatUsd(r.usage.cost_usd) : formatUsd(0)} {r.usage?.cost_usd === null && <Badge kind="unknown" />}
        </span>
      ),
      sortValue: (r) => r.usage?.cost_usd ?? -1,
    },
    {
      key: 'input',
      header: '입력',
      align: 'right',
      render: (r) => <span title={formatFull(r.usage?.input ?? 0)}>{formatCompact(r.usage?.input ?? 0)}</span>,
      sortValue: (r) => r.usage?.input ?? 0,
    },
    {
      key: 'output',
      header: '출력',
      align: 'right',
      render: (r) => <span title={formatFull(r.usage?.output ?? 0)}>{formatCompact(r.usage?.output ?? 0)}</span>,
      sortValue: (r) => r.usage?.output ?? 0,
    },
    {
      key: 'total_tokens',
      header: '토큰 합계',
      align: 'right',
      render: (r) => <span title={formatFull(tokens(r))}>{formatCompact(tokens(r))}</span>,
      sortValue: (r) => tokens(r),
    },
    { key: 'events', header: '이벤트', align: 'right', render: (r) => formatFull(r.usage?.events ?? 0), sortValue: (r) => r.usage?.events ?? 0 },
    {
      key: 'last_usage',
      header: '최근 사용',
      render: (r) => (r.usage ? formatDateTime(r.usage.last_seen) : '기간 내 사용 없음'),
      sortValue: (r) => r.usage?.last_seen ?? '',
    },
    {
      key: 'last_seen',
      header: '마지막 하트비트',
      render: (r) => (r.meta ? <span title={formatDateTime(r.meta.last_seen)}>{formatRelative(r.meta.last_seen)}</span> : '-'),
      sortValue: (r) => r.meta?.last_seen ?? '',
    },
  ]
  return (
    <>
      <DataTable columns={columns} rows={rows} rowKey={(r) => r.id} initialSortKey="cost" />
      <p className="table-footnote">
        토큰 재발급·재동기화·삭제는 상단의 <Link to="/manage/devices">기기 관리</Link> 메뉴에서 합니다.
      </p>
    </>
  )
}
