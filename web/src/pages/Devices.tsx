import { useState, type FormEvent } from 'react'
import { createDevice, deleteDevice, getDevices } from '../api/client'
import type { Device } from '../api/types'
import { Badge } from '../components/Badge'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { formatDateTime, formatFull, formatRelative, isStale } from '../lib/format'
import { useFilters } from '../state/FiltersContext'

export function Devices() {
  const { refreshKey, refresh } = useFilters()
  const state = useApiData(() => getDevices(), [refreshKey])
  const [newToken, setNewToken] = useState<{ name: string; token: string; id: string } | null>(null)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<string | null>(null)

  async function onCreate(e: FormEvent) {
    e.preventDefault()
    if (!newName.trim()) return
    setCreating(true)
    setCreateError(null)
    try {
      const res = await createDevice(newName.trim())
      setNewToken({ name: res.name, token: res.token, id: res.id })
      setNewName('')
      refresh()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : '기기 등록에 실패했습니다.')
    } finally {
      setCreating(false)
    }
  }

  async function onRevoke(id: string, name: string) {
    if (!window.confirm(`"${name}" 기기의 토큰을 폐기할까요? 이벤트 기록은 유지됩니다.`)) return
    setRevoking(id)
    try {
      await deleteDevice(id)
      refresh()
    } finally {
      setRevoking(null)
    }
  }

  const origin = typeof window !== 'undefined' ? window.location.origin : ''

  const columns: Column<Device>[] = [
    {
      key: 'name',
      header: '이름',
      render: (d) => (
        <span>
          {d.name} {isStale(d.last_seen) && <Badge kind="stale" />}
        </span>
      ),
      sortValue: (d) => d.name,
    },
    { key: 'os', header: 'OS / Arch', render: (d) => `${d.os}/${d.arch}`, sortValue: (d) => `${d.os}/${d.arch}` },
    { key: 'agent_version', header: '에이전트 버전', render: (d) => d.agent_version, sortValue: (d) => d.agent_version },
    {
      key: 'last_seen',
      header: '마지막 하트비트',
      render: (d) => (
        <span title={formatDateTime(d.last_seen)}>{formatRelative(d.last_seen)}</span>
      ),
      sortValue: (d) => d.last_seen,
    },
    { key: 'events_total', header: '누적 이벤트', align: 'right', render: (d) => formatFull(d.events_total), sortValue: (d) => d.events_total },
    { key: 'token_created_at', header: '토큰 발급일', render: (d) => formatDateTime(d.token_created_at), sortValue: (d) => d.token_created_at },
    {
      key: 'actions',
      header: '',
      render: (d) => (
        <button type="button" className="btn btn-sm btn-danger" disabled={revoking === d.id} onClick={() => onRevoke(d.id, d.name)}>
          {revoking === d.id ? '폐기 중…' : '토큰 폐기'}
        </button>
      ),
    },
  ]

  return (
    <div>
      <div className="page-header">
        <h1>기기</h1>
      </div>

      <div className="card section">
        <h2>기기 추가</h2>
        <form className="field-row" onSubmit={onCreate}>
          <div className="field">
            <label htmlFor="device-name">이름</label>
            <input id="device-name" value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="예: macbook-pro" />
          </div>
          <button type="submit" className="btn btn-primary" disabled={creating || !newName.trim()}>
            {creating ? '등록 중…' : '추가'}
          </button>
        </form>
        {createError && <p className="login-error">{createError}</p>}

        {newToken && (
          <div className="copy-box">
            <strong>{newToken.name}</strong>의 토큰이 발급되었습니다. 이 토큰은 다시 표시되지 않으니 지금 복사하세요.
            <div className="copy-box-row">
              <code>{newToken.token}</code>
              <button type="button" className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(newToken.token)}>
                복사
              </button>
            </div>
            <div className="copy-box-row">
              <code>
                curl -fsSL {origin}/downloads/install.sh | sh -s -- --server {origin} --token {newToken.token}
              </code>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() =>
                  navigator.clipboard?.writeText(`curl -fsSL ${origin}/downloads/install.sh | sh -s -- --server ${origin} --token ${newToken.token}`)
                }
              >
                복사
              </button>
            </div>
          </div>
        )}
      </div>

      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && state.data.devices.length === 0}
        emptyMessage="등록된 기기가 없습니다."
      >
        {state.status === 'success' && <DataTable columns={columns} rows={state.data.devices} rowKey={(d) => d.id} initialSortKey="last_seen" />}
      </StateContainer>
    </div>
  )
}
