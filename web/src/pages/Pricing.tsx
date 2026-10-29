import { useState } from 'react'
import { getPricing, reloadPricing } from '../api/client'
import type { PricingRate } from '../api/types'
import { Badge } from '../components/Badge'
import { type Column, DataTable } from '../components/DataTable'
import { PageHeader } from '../components/PageHeader'
import { StateContainer } from '../components/StateContainer'
import { Button } from '../components/ui/button'
import { useApiData } from '../hooks/useApiData'
import { describeError } from '../lib/errors'

/** Any pricing rate field can legitimately be `null` (unknown) or absent
 * entirely — never call `.toFixed` without checking both. Unlike `formatUsd`
 * these are per-1M-token list prices, always shown at 2 decimals. */
function fmtRate(v: number | null | undefined): string {
  return v === null || v === undefined ? '—' : `$${v.toFixed(2)}`
}

/** A per-1M-token rate column; unknown sorts below zero. */
function rateColumn(key: string, header: string, value: (r: PricingRate) => number | null | undefined): Column<PricingRate> {
  return { key, header, align: 'right', render: (r) => fmtRate(value(r)), sortValue: (r) => value(r) ?? -1 }
}

const PRICING_COLUMNS: Column<PricingRate>[] = [
  {
    key: 'match',
    header: '매칭 패턴',
    render: (r) => (
      <span className="font-mono">
        {r.match} {r.estimated && <Badge kind="estimated" />} {r.alias_of && <Badge kind="alias" title={`→ ${r.alias_of}`} />}
      </span>
    ),
    sortValue: (r) => r.match,
  },
  { key: 'provider', header: '제공자', render: (r) => r.provider ?? '-', sortValue: (r) => r.provider ?? '' },
  rateColumn('input', '입력 ($/1M)', (r) => r.input),
  rateColumn('output', '출력 ($/1M)', (r) => r.output),
  rateColumn('cache_read', '캐시 읽기 ($/1M)', (r) => r.cache_read),
  rateColumn('cache_write', '캐시 쓰기 ($/1M)', (r) => r.cache_write ?? r.cache_write_5m),
  rateColumn('cache_write_1h', '캐시 쓰기 1h ($/1M)', (r) => r.cache_write_1h),
  { key: 'alias_of', header: '별칭', render: (r) => r.alias_of ?? '-', sortValue: (r) => r.alias_of ?? '' },
  {
    key: 'period',
    header: '적용 기간',
    render: (r) =>
      r.effective_from || r.effective_to ? (
        <span title={r.note ?? undefined}>
          {r.effective_from ?? '…'} ~ {r.effective_to ?? ''}
          {r.note && <span className="text-fg3"> ({r.note})</span>}
        </span>
      ) : (
        <span className="text-fg3">상시{r.note ? ` (${r.note})` : ''}</span>
      ),
    sortValue: (r) => r.effective_from ?? '',
  },
]

export function Pricing() {
  const state = useApiData(() => getPricing(), [])
  const [reloading, setReloading] = useState(false)
  const [reloadError, setReloadError] = useState<string | null>(null)

  async function onReload() {
    setReloading(true)
    setReloadError(null)
    try {
      await reloadPricing()
      state.reload()
    } catch (err) {
      setReloadError(describeError(err))
    } finally {
      setReloading(false)
    }
  }

  return (
    <div>
      <PageHeader title="가격표">
        <Button touch onClick={onReload} disabled={reloading}>
          {reloading ? '재로드 중…' : 'pricing.yaml 다시 읽기'}
        </Button>
      </PageHeader>
      {reloadError && <p className="text-200 text-danger">{reloadError}</p>}
      <p className="text-fg3">
        입력/출력 단가가 <strong>—</strong>로 표시되는 모델은 단가를 알 수 없어 비용 합계에서 <Badge kind="unknown" /> (단가 미상)으로 집계됩니다.{' '}
        <Badge kind="estimated" />는 alias 단가로 추정 계산된 모델, <Badge kind="alias" />는 다른 모델의 단가를 그대로 사용하는 별칭입니다.
      </p>

      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.rates.length === 0}
        emptyMessage="가격표 항목이 없습니다."
      >
        {state.status === 'success' && (
          <DataTable
            columns={PRICING_COLUMNS}
            rows={state.data.rates}
            rowKey={(r) => `${r.match}:${r.effective_from ?? ''}:${r.effective_to ?? ''}`}
            initialSortKey="match"
            initialSortDir="asc"
            size="small"
          />
        )}
      </StateContainer>
    </div>
  )
}
