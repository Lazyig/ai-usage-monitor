import { useMemo, useState, type FormEvent } from 'react'
import { createDevice, deleteDevice, getDevices, purgeDevice, purgeDeviceEvents, rotateDevice } from '../api/client'
import type { Device } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { describeError } from '../lib/errors'
import { formatDateTime, formatFull, formatRelative, isStale } from '../lib/format'
import { installCommands } from '../lib/installCommands'
import { useFilters } from '../state/FiltersContext'

interface ConfirmState {
  kind: 'resync' | 'purge'
  device: Device
}

export function Devices() {
  const { refreshKey, refresh } = useFilters()
  const state = useApiData(() => getDevices(), [refreshKey])
  const [newToken, setNewToken] = useState<{ name: string; token: string; id: string } | null>(null)
  const [newName, setNewName] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [revoking, setRevoking] = useState<string | null>(null)
  const [rotating, setRotating] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionNotice, setActionNotice] = useState<string | null>(null)
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [confirming, setConfirming] = useState(false)
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
    setActionError(null)
    setActionNotice(null)
    try {
      await deleteDevice(id)
      state.reload()
      setActionNotice('토큰을 폐기했습니다.')
    } catch (err) {
      setActionError(describeError(err))
    } finally {
      setRevoking(null)
    }
  }

  async function onRotate(d: Device) {
    if (!window.confirm(`"${d.name}" 기기의 토큰을 재발급할까요? 기존 토큰은 즉시 사용할 수 없게 됩니다.`)) return
    setRotating(d.id)
    setActionError(null)
    setActionNotice(null)
    try {
      const res = await rotateDevice(d.id)
      setNewToken({ name: res.name, token: res.token, id: res.id })
      state.reload()
    } catch (err) {
      setActionError(describeError(err))
    } finally {
      setRotating(null)
    }
  }

  async function onConfirmAction() {
    if (!confirmState) return
    setConfirming(true)
    setActionError(null)
    setActionNotice(null)
    try {
      if (confirmState.kind === 'resync') {
        const res = await purgeDeviceEvents(confirmState.device.id)
        setActionNotice(`이벤트 ${formatFull(res.deleted_events)}건을 삭제하고 재동기화를 요청했습니다.`)
      } else {
        const res = await purgeDevice(confirmState.device.id)
        setActionNotice(`이벤트 ${formatFull(res.deleted_events)}건과 함께 기기를 완전히 삭제했습니다.`)
      }
      state.reload()
      setConfirmState(null)
    } catch (err) {
      setActionError(describeError(err))
    } finally {
      setConfirming(false)
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
      render: (d) => (
        <div className="action-group">
          <button type="button" className="btn btn-sm" disabled={rotating === d.id} onClick={() => onRotate(d)}>
            {rotating === d.id ? '재발급 중…' : '토큰 재발급'}
          </button>
          <button
            type="button"
            className="btn btn-sm"
            disabled={!!d.revoked_at}
            title={d.revoked_at ? '폐기된 기기는 재동기화할 수 없습니다. 먼저 토큰을 재발급하세요.' : undefined}
            onClick={() => setConfirmState({ kind: 'resync', device: d })}
          >
            재동기화
          </button>
          {d.revoked_at ? (
            <button type="button" className="btn btn-sm btn-danger" disabled>
              폐기됨
            </button>
          ) : (
            <button type="button" className="btn btn-sm btn-danger" disabled={revoking === d.id} onClick={() => onRevoke(d.id, d.name)}>
              {revoking === d.id ? '폐기 중…' : '토큰 폐기'}
            </button>
          )}
          <button type="button" className="btn btn-sm btn-danger" onClick={() => setConfirmState({ kind: 'purge', device: d })}>
            완전 삭제
          </button>
        </div>
      ),
    },
  ]

  return (
    <div>
      <div className="page-header">
        <h1>기기</h1>
      </div>

      <ul className="help-list">
        <li>
          <strong>토큰 재발급</strong> — 기존 토큰을 즉시 무효화하고 새 토큰을 발급합니다. 기기 이력은 그대로 유지되며, 폐기된 기기도 다시 살아납니다.
        </li>
        <li>
          <strong>재동기화</strong> — 이 기기의 이벤트를 모두 지우고, 에이전트가 다음 하트비트(1분 이내)에 전체 이력을 다시 보내도록 합니다.
        </li>
        <li>
          <strong>토큰 폐기</strong> — 토큰만 무효화합니다. 이벤트 기록은 그대로 유지됩니다.
        </li>
        <li>
          <strong>완전 삭제</strong> — 기기와 이 기기의 모든 이벤트를 영구적으로 삭제합니다.
        </li>
      </ul>

      <Card className="section" title="기기 추가">
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
      </Card>

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

      {actionNotice && <p className="toast-success">{actionNotice}</p>}
      {actionError && (
        <p className="login-error" role="alert">
          작업에 실패했습니다: {actionError}
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

      {confirmState?.kind === 'resync' && (
        <ConfirmDialog
          title="재동기화"
          message={`"${confirmState.device.name}" 기기의 이벤트를 모두 삭제합니다. 이 기기의 통계가 잠시 0 이 되었다가 에이전트가 1분 안에 다시 보냅니다.`}
          confirmLabel="재동기화"
          busy={confirming}
          onConfirm={onConfirmAction}
          onCancel={() => setConfirmState(null)}
        />
      )}
      {confirmState?.kind === 'purge' && (
        <ConfirmDialog
          title="완전 삭제"
          message="이 기기의 모든 이벤트가 통계에서 사라집니다. 되돌릴 수 없습니다."
          confirmLabel="완전 삭제"
          requireText={confirmState.device.name}
          busy={confirming}
          onConfirm={onConfirmAction}
          onCancel={() => setConfirmState(null)}
        />
      )}
    </div>
  )
}
