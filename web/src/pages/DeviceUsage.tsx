import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { getBreakdown, getDevices, getTimeseries } from '../api/client'
import type { BreakdownRow, Device, Source, TimeseriesBucket } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { CompositionChart, CompositionLegend, type CompositionSegment } from '../components/CompositionChart'
import { type Column, DataTable } from '../components/DataTable'
import { ErrorBoundary } from '../components/ErrorBoundary'
import { RowField } from '../components/RowField'
import { SegmentedControl } from '../components/SegmentedControl'
import { StatTile } from '../components/StatTile'
import { StateContainer } from '../components/StateContainer'
import { TrendChart } from '../components/TrendChart'
import { costColumn, countColumn, Swatch, tokenColumn } from '../components/columns'
import { Button } from '../components/ui/button'
import { NativeSelect } from '../components/ui/native-select'
import { useApiData } from '../hooks/useApiData'
import { type Metric, METRIC_OPTIONS, BUCKET_OPTIONS, metricFormat, seriesTokens, topBy } from '../lib/chartData'
import { SOURCE_COLOR_VARS, SOURCE_ORDER, colorForSlot, labelForKey } from '../lib/colors'
import { hourlyAllowed } from '../lib/dateRange'
import { formatCompact, formatDateTime, formatFull, formatRelative, formatUsd, isStale } from '../lib/format'
import { buildMatrix, fetchSourceDeviceMatrix } from '../lib/matrix'
import { tokensOf } from '../lib/usage'
import { useFilters } from '../state/FiltersContext'

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

  const canBucketHourly = hourlyAllowed(range)
  const effectiveBucket: TimeseriesBucket = canBucketHourly ? bucket : 'day'

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
    () => (source ? [source] : SOURCE_ORDER).map((s) => ({ key: s, label: labelForKey(s), color: SOURCE_COLOR_VARS[s] })),
    [source],
  )
  const mixRows = useMemo(() => (matrix?.devices ?? []).map((d) => ({ key: d.id, label: d.name })), [matrix])

  // Only the busiest devices get a band in the trend chart.
  const trendSeries = useMemo(
    () => (trendState.status === 'success' ? topBy(trendState.data.series, MAX_TREND_DEVICES, seriesTokens) : []),
    [trendState],
  )
  const trendTruncated = trendState.status === 'success' && trendState.data.series.length > trendSeries.length

  const usageError = usageState.status === 'error' ? usageState.message : null

  return (
    <div>
      <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
        <h1>기기</h1>
      </div>

      <div className="mb-4 flex flex-wrap items-end gap-3 max-phone:flex-col max-phone:items-stretch">
        <RowField label="에이전트">
          <NativeSelect id="device-source-filter" value={source} onChange={(e) => setSource(e.target.value as Source | '')}>
            <option value="">전체 에이전트</option>
            {SOURCE_ORDER.map((s) => (
              <option key={s} value={s}>
                {labelForKey(s)}
              </option>
            ))}
          </NativeSelect>
        </RowField>
        {source && (
          <Button touch size="small" appearance="subtle" onClick={() => setSource('')}>
            필터 해제
          </Button>
        )}
      </div>

      <ErrorBoundary label="통계 타일을 표시할 수 없습니다">
        <StateContainer loading={usageState.status === 'loading'} error={usageError} onRetry={usageState.reload}>
          {usageState.status === 'success' && (
            <div className="mb-6 grid grid-cols-[repeat(auto-fit,minmax(130px,1fr))] gap-3 max-tablet:grid-cols-[repeat(2,1fr)] max-tiny:grid-cols-[1fr]">
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
        className="mb-6"
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
                  formatValue={metricFormat(mixMetric)}
                />
                <CompositionLegend segments={mixSegments} />
              </>
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <Card
        className="mb-6"
        title="기기별 사용량 추이"
        subtitle={trendTruncated ? `상위 ${MAX_TREND_DEVICES}대만 표시` : undefined}
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
            empty={trendState.status === 'success' && trendSeries.length === 0}
          >
            {trendSeries.length > 0 && trendState.status === 'success' && (
              <TrendChart
                series={trendSeries}
                mode={trendMetric}
                labelFor={nameOf}
                colorFor={colorOf}
                span={{ range, bucket: trendState.data.bucket }}
              />
            )}
          </StateContainer>
        </ErrorBoundary>
      </Card>

      <h2>기기별 상세</h2>
      <StateContainer
        loading={usageState.status === 'loading' || devicesState.status === 'loading'}
        error={usageError ?? (devicesState.status === 'error' ? devicesState.message : null)}
        onRetry={usageState.reload}
        empty={usageState.status === 'success' && rows.length === 0}
        emptyMessage="등록된 기기가 없습니다."
      >
        {usageState.status === 'success' && rows.length > 0 && <DeviceUsageTable rows={rows} colorOf={colorOf} />}
      </StateContainer>
    </div>
  )
}

function deviceColumns(colorOf: (id: string) => string): Column<DeviceUsageRow>[] {
  return [
    {
      key: 'name',
      header: '기기',
      media: (r) => <Swatch color={colorOf(r.id)} />,
      render: (r) => (
        <span>
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
    // A row with no usage in the range shows $0.00 rather than "—": the device
    // is known to have spent nothing, which is not the same as unknown price.
    costColumn('cost', '비용', (r) => (r.usage ? r.usage.cost_usd : 0), {
      badge: true,
      ratio: (r) => r.usage?.cost_known_ratio,
      sortValue: (r) => r.usage?.cost_usd ?? -1,
    }),
    tokenColumn('input', '입력', (r) => r.usage?.input ?? 0),
    tokenColumn('output', '출력', (r) => r.usage?.output ?? 0),
    tokenColumn('total_tokens', '토큰 합계', (r) => (r.usage ? tokensOf(r.usage) : 0)),
    countColumn('events', '이벤트', (r) => r.usage?.events ?? 0),
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
}

function DeviceUsageTable({ rows, colorOf }: { rows: DeviceUsageRow[]; colorOf: (id: string) => string }) {
  return (
    <>
      <DataTable columns={deviceColumns(colorOf)} rows={rows} rowKey={(r) => r.id} initialSortKey="cost" />
      <p className="mx-0 mt-1 mb-0 text-200 text-fg3">
        토큰 재발급·재동기화·삭제는 상단의 <Link to="/manage/devices">기기 관리</Link> 메뉴에서 합니다.
      </p>
    </>
  )
}
