import { useMemo, useState, type FormEvent } from 'react'
import { createDevice, deleteDevice, getDevices } from '../api/client'
import type { Device } from '../api/types'
import { Badge } from '../components/Badge'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { describeError } from '../lib/errors'
import { formatDateTime, formatFull, formatRelative, isStale } from '../lib/format'
import { installCommands } from '../lib/installCommands'
import { useFilters } from '../state/FiltersContext'

export function Devices() {
  const { refreshKey, refresh } = useFilters()
  const state = useApiData(() => getDevices(), [refreshKey])
  const [newToken, setNewToken] = useState<{ name: string; token: string; id: string } | null>(null)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<string | null>(null)
  const [revokeError, setRevokeError] = useState<string | null>(null)
  const [revokeNotice, setRevokeNotice] = useState<string | null>(null)
  const [showRevoked, setShowRevoked] = useState(false)

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
      setCreateError(describeError(err))
    } finally {
      setCreating(false)
    }
  }

  async function onRevoke(id: string, name: string) {
    if (!window.confirm(`"${name}" 기기의 토큰을 폐기할까요? 이벤트 기록은 유지됩니다.`)) return
    setRevoking(id)
    setRevokeError(null)
    setRevokeNotice(null)
    try {
      await deleteDevice(id)
      state.reload()
      setRevokeNotice('토큰을 폐기했습니다.')
    } catch (err) {
      setRevokeError(describeError(err))
    } finally {
      setRevoking(null)
    }
  }

  const origin = typeof window !== 'undefined' ? window.location.origin : ''

  const visibleDevices = useMemo(() => {
    if (state.status !== 'success') return []
    return showRevoked ? state.data.devices : state.data.devices.filter((d) => !d.revoked_at)
  }, [state, showRevoked])

  const revokedCount = state.status === 'success' ? state.data.devices.filter((d) => d.revoked_at).length : 0

  const columns: Column<Device>[] = [
    {
      key: 'name',
      header: '이름',
      render: (d) => (
        <span>
          {d.name} {!d.revoked_at && isStale(d.last_seen) && <Badge kind="stale" />}{' '}
          {d.revoked_at && <Badge kind="revoked" title={`폐기 시각: ${formatDateTime(d.revoked_at)}`} />}
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
      key: 'revoked_at',
      header: '폐기 시각',
      render: (d) => (d.revoked_at ? formatDateTime(d.revoked_at) : '-'),
      sortValue: (d) => d.revoked_at ?? '',
    },
    {
      key: 'actions',
      header: '',
      render: (d) =>
        d.revoked_at ? (
          <button type="button" className="btn btn-sm btn-danger" disabled>
            폐기됨
          </button>
        ) : (
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

        {newToken &&
          (() => {
            const cmds = installCommands(origin, newToken.token)
            return (
              <div className="copy-box">
                <strong>{newToken.name}</strong>의 토큰이 발급되었습니다. 이 토큰은 다시 표시되지 않으니 지금 복사하세요.
                <div className="copy-box-row">
                  <code>{newToken.token}</code>
                  <button type="button" className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(newToken.token)}>
                    복사
                  </button>
                </div>
                <p style={{ marginTop: 10, marginBottom: 2 }}>Linux / macOS 설치 명령</p>
                <div className="copy-box-row">
                  <code>{cmds.unix}</code>
                  <button type="button" className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(cmds.unix)}>
                    복사
                  </button>
                </div>
                <p style={{ marginTop: 10, marginBottom: 2 }}>Windows (PowerShell) 설치 명령</p>
                <div className="copy-box-row">
                  <code>{cmds.windows}</code>
                  <button type="button" className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(cmds.windows)}>
                    복사
                  </button>
                </div>
              </div>
            )
          })()}
      </div>

      {revokeNotice && <p className="toast-success">{revokeNotice}</p>}
      {revokeError && (
        <p className="login-error" role="alert">
          토큰 폐기에 실패했습니다: {revokeError}
        </p>
      )}

      <label className="toggle-row">
        <input type="checkbox" checked={showRevoked} onChange={(e) => setShowRevoked(e.target.checked)} />
        폐기된 기기 표시{revokedCount > 0 ? ` (${revokedCount})` : ''}
      </label>

      <StateContainer
        loading={state.status === 'loading'}
        error={state.status === 'error' ? state.message : null}
        onRetry={state.reload}
        empty={state.status === 'success' && visibleDevices.length === 0}
        emptyMessage={showRevoked || revokedCount === 0 ? '등록된 기기가 없습니다.' : '표시할 기기가 없습니다 (폐기된 기기는 숨겨져 있습니다).'}
      >
        {state.status === 'success' && <DataTable columns={columns} rows={visibleDevices} rowKey={(d) => d.id} initialSortKey="last_seen" />}
      </StateContainer>
    </div>
  )
}
