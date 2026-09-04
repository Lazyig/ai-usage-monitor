import { useMemo, useState } from 'react'
import type { ThroughputGroup, ThroughputPeak, ThroughputResponse } from '../api/types'
import { formatCompact, formatFull, formatShortDateTime } from '../lib/format'
import { Card } from './Card'
import { type Column, DataTable } from './DataTable'
import { ErrorBoundary } from './ErrorBoundary'
import { StateContainer } from './StateContainer'
import { StatTile } from './StatTile'
import { countColumn, tokenColumn } from './columns'

interface ThroughputCardProps {
  title: string
  loading: boolean
  error: string | null
  onRetry: () => void
  data: ThroughputResponse | null
  /** Display name of a group row (agent label, model name, device name…). */
  labelFor: (group: ThroughputGroup) => string
  /** Header of the first column. */
  keyHeader: string
  /** Rows shown before the "더 보기" toggle; omit to show every group. */
  limit?: number
}

/**
 * Tokens per active minute for one grouping: four headline tiles for the whole
 * range, then a per-group table with the averages and the peak minute of prompt
 * and output tokens. The peak columns are the ones to hold against a provider's
 * per-minute rate limits (ITPM/OTPM).
 */
export function ThroughputCard({ title, loading, error, onRetry, data, labelFor, keyHeader, limit }: ThroughputCardProps) {
  const [showAll, setShowAll] = useState(false)
  const groups = data?.groups ?? []
  const visible = limit && !showAll ? groups.slice(0, limit) : groups
  const hidden = groups.length - visible.length

  const columns = useMemo<Column<ThroughputGroup>[]>(
    () => [
      { key: 'name', header: keyHeader, render: (g) => labelFor(g), sortValue: (g) => labelFor(g) },
      countColumn('active', '활동 분', (g) => g.active_minutes),
      tokenColumn('pm_prompt', '분당 프롬프트', (g) => g.per_minute.prompt),
      tokenColumn('pm_output', '분당 출력', (g) => g.per_minute.output),
      tokenColumn('pm_total', '분당 합계', (g) => g.per_minute.total),
      peakColumn('peak_prompt', '피크 프롬프트/분', (g) => g.peak_prompt),
      peakColumn('peak_output', '피크 출력/분', (g) => g.peak_output),
    ],
    [keyHeader, labelFor],
  )

  const total = data?.total
  const empty = !!data && groups.length === 0

  return (
    <Card className="section" title={title} subtitle="이벤트가 완료된 분만 활동 분으로 계산 · 유휴 시간 제외">
      <ErrorBoundary label="처리량을 표시할 수 없습니다">
        <StateContainer loading={loading} error={error} onRetry={onRetry} empty={empty} emptyMessage="해당 기간에 사용 내역이 없습니다.">
          {data && total && groups.length > 0 && (
            <>
              <div className="stat-tiles">
                <StatTile
                  label="활동 분"
                  value={formatFull(total.active_minutes)}
                  hint={`이벤트 ${formatFull(total.events)}건`}
                  title="이벤트가 하나라도 완료된 1분 구간의 수"
                />
                <StatTile
                  label="분당 프롬프트"
                  value={formatCompact(total.per_minute.prompt)}
                  hint="입력 + 캐시 읽기 + 캐시 쓰기"
                  title={formatFull(total.per_minute.prompt)}
                />
                <StatTile label="분당 출력" value={formatCompact(total.per_minute.output)} hint="활동 분 평균" title={formatFull(total.per_minute.output)} />
                <StatTile
                  label="피크 출력/분"
                  value={formatCompact(total.peak_output?.tokens ?? 0)}
                  hint={total.peak_output ? formatShortDateTime(total.peak_output.t) : undefined}
                  title={total.peak_output ? formatFull(total.peak_output.tokens) : undefined}
                />
              </div>
              <DataTable columns={columns} rows={visible} rowKey={(g) => g.key} initialSortKey="pm_total" />
              {(hidden > 0 || showAll) && limit && groups.length > limit && (
                <button type="button" className="btn btn-sm btn-ghost" style={{ marginTop: 8 }} onClick={() => setShowAll((v) => !v)}>
                  {showAll ? `상위 ${limit}개만 보기` : `${hidden}개 더 보기`}
                </button>
              )}
              <p className="throughput-note">
                토큰은 응답이 완료된 분에 귀속됩니다. 요청 시작 시각은 수집하지 않으므로 응답 생성 속도가 아니라 분당 완료량의 근사치입니다.
              </p>
            </>
          )}
        </StateContainer>
      </ErrorBoundary>
    </Card>
  )
}

/** Compact peak count with the minute it happened underneath; sorts by count. */
function peakColumn(key: string, header: string, value: (g: ThroughputGroup) => ThroughputPeak | undefined): Column<ThroughputGroup> {
  return {
    key,
    header,
    align: 'right',
    render: (g) => {
      const p = value(g)
      if (!p) return '—'
      return (
        <span title={formatFull(p.tokens)}>
          {formatCompact(p.tokens)}
          <span className="peak-time">{formatShortDateTime(p.t)}</span>
        </span>
      )
    },
    sortValue: (g) => value(g)?.tokens ?? 0,
  }
}
