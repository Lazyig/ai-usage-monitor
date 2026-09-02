import { useState } from 'react'
import { getPricing, reloadPricing } from '../api/client'
import type { PricingRate } from '../api/types'
import { Badge } from '../components/Badge'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'

function fmtRate(v: number | undefined): string {
  return v === undefined ? '—' : `$${v.toFixed(2)}`
}

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
      setReloadError(err instanceof Error ? err.message : '가격표 재로드에 실패했습니다.')
    } finally {
      setReloading(false)
    }
  }

  const columns: Column<PricingRate>[] = [
    {
      key: 'match',
      header: '매칭 패턴',
      render: (r) => (
        <span className="mono">
          {r.match} {r.estimated && <Badge kind="estimated" />}
        </span>
      ),
      sortValue: (r) => r.match,
    },
    { key: 'provider', header: '제공자', render: (r) => r.provider ?? '-', sortValue: (r) => r.provider ?? '' },
    { key: 'input', header: '입력 ($/1M)', align: 'right', render: (r) => fmtRate(r.input), sortValue: (r) => r.input ?? -1 },
    { key: 'output', header: '출력 ($/1M)', align: 'right', render: (r) => fmtRate(r.output), sortValue: (r) => r.output ?? -1 },
    { key: 'cache_read', header: '캐시 읽기 ($/1M)', align: 'right', render: (r) => fmtRate(r.cache_read), sortValue: (r) => r.cache_read ?? -1 },
    {
      key: 'cache_write',
      header: '캐시 쓰기 ($/1M)',
      align: 'right',
      render: (r) => fmtRate(r.cache_write ?? r.cache_write_5m),
      sortValue: (r) => r.cache_write ?? r.cache_write_5m ?? -1,
    },
    { key: 'cache_write_1h', header: '캐시 쓰기 1h ($/1M)', align: 'right', render: (r) => fmtRate(r.cache_write_1h), sortValue: (r) => r.cache_write_1h ?? -1 },
    { key: 'alias_of', header: '별칭', render: (r) => r.alias_of ?? '-', sortValue: (r) => r.alias_of ?? '' },
  ]

  return (
    <div>
      <div className="page-header">
        <h1>가격표</h1>
        <button type="button" className="btn" onClick={onReload} disabled={reloading}>
          {reloading ? '재로드 중…' : 'pricing.yaml 다시 읽기'}
        </button>
      </div>
      {reloadError && <p className="login-error">{reloadError}</p>}

      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.rates.length === 0}
        emptyMessage="가격표 항목이 없습니다."
      >
        {state.status === 'success' && <DataTable columns={columns} rows={state.data.rates} rowKey={(r) => r.match} initialSortKey="match" initialSortDir="asc" />}
      </StateContainer>
    </div>
  )
}
