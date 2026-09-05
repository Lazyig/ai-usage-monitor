import { useMemo, useState, type FormEvent } from 'react'
import { createDevice, renameDevice, deleteDevice, getDevices, purgeDevice, purgeDeviceEvents, requestResync, rotateDevice } from '../api/client'
import type { Device } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { PromptDialog } from '../components/PromptDialog'
import { type Column, DataTable } from '../components/DataTable'
import { countColumn, dateTimeColumn } from '../components/columns'
import { StateContainer } from '../components/StateContainer'
import { useApiData } from '../hooks/useApiData'
import { describeError } from '../lib/errors'
import { formatDateTime, formatFull, formatRelative, isStale } from '../lib/format'
import { installCommands } from '../lib/installCommands'
import { useFilters } from '../state/FiltersContext'

interface ConfirmState {
  kind: 'resync' | 'reset' | 'purge' | 'rotate' | 'revoke' | 'rename'
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

  async function performRevoke(d: Device) {
    setRevoking(d.id)
    setActionError(null)
    setActionNotice(null)
    try {
      await deleteDevice(d.id)
      state.reload()
      setActionNotice(`"${d.name}" 기기의 토큰을 폐기했습니다.`)
      setConfirmState(null)
    } catch (err) {
      setActionError(describeError(err))
    } finally {
      setRevoking(null)
    }
  }

  async function performRename(d: Device, name: string) {
    setConfirming(true)
    setActionError(null)
    setActionNotice(null)
    try {
      await renameDevice(d.id, name)
      setActionNotice(`이름을 "${name}" 으로 바꿨습니다.`)
      state.reload()
      setConfirmState(null)
    } catch (err) {
      setActionError(describeError(err))
    } finally {
      setConfirming(false)
    }
  }

  async function performRotate(d: Device) {
    setRotating(d.id)
    setActionError(null)
    setActionNotice(null)
    try {
      const res = await rotateDevice(d.id)
      setNewToken({ name: res.name, token: res.token, id: res.id })
      state.reload()
      setConfirmState(null)
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
        await requestResync(confirmState.device.id)
        setActionNotice('재동기화를 요청했습니다. 에이전트가 1분 안에 로그를 처음부터 다시 보내며, 기존 통계는 그대로 유지됩니다.')
      } else if (confirmState.kind === 'reset') {
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
        <div className="name-cell">
          <span>
            {d.name} {!d.revoked_at && isStale(d.last_seen) && <Badge kind="stale" />}{' '}
            {d.revoked_at && <Badge kind="revoked" title={`폐기 시각: ${formatDateTime(d.revoked_at)}`} />}
            {d.hostname && d.hostname !== d.name && (
              <span className="muted name-cell-hostname" title="에이전트가 보고한 호스트명">
                {d.hostname}
              </span>
            )}
          </span>
          <button
            type="button"
            className="icon-btn"
            aria-label={`"${d.name}" 이름 변경`}
            title="이름 변경"
            onClick={() => setConfirmState({ kind: 'rename', device: d })}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M12 20h9" />
              <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" />
            </svg>
          </button>
        </div>
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
    countColumn('events_total', '누적 이벤트', (d) => d.events_total),
    dateTimeColumn('token_created_at', '토큰 발급일', (d) => d.token_created_at),
    dateTimeColumn('revoked_at', '폐기 시각', (d) => d.revoked_at, { fallback: '-' }),
    {
      key: 'actions',
      header: '',
      render: (d) => (
        <div className="action-group">
          <button type="button" className="btn btn-sm" disabled={rotating === d.id} onClick={() => setConfirmState({ kind: 'rotate', device: d })}>
            {rotating === d.id ? '재발급 중…' : '토큰 재발급'}
          </button>
          <button
            type="button"
            className="btn btn-sm"
            disabled={!!d.revoked_at}
            title={d.revoked_at ? '폐기된 기기는 재동기화할 수 없습니다. 먼저 토큰을 재발급하세요.' : '삭제 없이 로그를 처음부터 다시 보내게 합니다'}
            onClick={() => setConfirmState({ kind: 'resync', device: d })}
          >
            재동기화
          </button>
          <button
            type="button"
            className="btn btn-sm btn-danger"
            disabled={!!d.revoked_at}
            title={d.revoked_at ? '폐기된 기기는 초기화할 수 없습니다. 먼저 토큰을 재발급하세요.' : '이 기기의 이벤트를 모두 지운 뒤 다시 받습니다'}
            onClick={() => setConfirmState({ kind: 'reset', device: d })}
          >
            초기화
          </button>
          {d.revoked_at ? (
            <button type="button" className="btn btn-sm btn-danger" disabled>
              폐기됨
            </button>
          ) : (
            <button type="button" className="btn btn-sm btn-danger" disabled={revoking === d.id} onClick={() => setConfirmState({ kind: 'revoke', device: d })}>
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
              <p className="admin-note" role="note">
                <strong>관리자 권한 필수</strong> — PowerShell 을 &quot;관리자 권한으로 실행&quot; 한 창에서 실행하세요. 일반 권한에서는 Windows 서비스 등록이 실패해
                에이전트가 자동 시작되지 않습니다.
              </p>
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
          message={`"${confirmState.device.name}" 기기의 에이전트가 로그를 처음부터 다시 보냅니다. 기존 이벤트는 지우지 않으며, 이미 있는 이벤트는 무시되고 스트리밍 도중 잘려서 적게 잡힌 응답만 보정됩니다.`}
          confirmLabel="재동기화"
          danger={false}
          busy={confirming}
          onConfirm={onConfirmAction}
          onCancel={() => setConfirmState(null)}
        />
      )}
      {confirmState?.kind === 'reset' && (
        <ConfirmDialog
          title="초기화"
          message={`"${confirmState.device.name}" 기기의 이벤트를 모두 삭제합니다. 이 기기의 통계가 잠시 0 이 되었다가 에이전트가 1분 안에 다시 보냅니다.`}
          confirmLabel="초기화"
          busy={confirming}
          onConfirm={onConfirmAction}
          onCancel={() => setConfirmState(null)}
        />
      )}
      {confirmState?.kind === 'rotate' && (
        <ConfirmDialog
          title="토큰 재발급"
          message={`"${confirmState.device.name}" 기기의 토큰을 재발급합니다. 기존 토큰은 즉시 사용할 수 없게 되며, 새 토큰으로 에이전트를 다시 설치해야 합니다.`}
          confirmLabel="재발급"
          danger={false}
          busy={rotating === confirmState.device.id}
          onConfirm={() => performRotate(confirmState.device)}
          onCancel={() => setConfirmState(null)}
        />
      )}
      {confirmState?.kind === 'revoke' && (
        <ConfirmDialog
          title="토큰 폐기"
          message={`"${confirmState.device.name}" 기기의 토큰을 폐기합니다. 이벤트 기록은 유지되며, 토큰 재발급으로 되살릴 수 있습니다.`}
          confirmLabel="토큰 폐기"
          busy={revoking === confirmState.device.id}
          onConfirm={() => performRevoke(confirmState.device)}
          onCancel={() => setConfirmState(null)}
        />
      )}
      {confirmState?.kind === 'rename' && (
        <PromptDialog
          title="기기 이름 변경"
          message="에이전트가 보고하는 호스트명과 별개로 유지되는 표시 이름입니다."
          label="새 이름"
          initialValue={confirmState.device.name}
          confirmLabel="변경"
          busy={confirming}
          onConfirm={(name) => performRename(confirmState.device, name)}
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
