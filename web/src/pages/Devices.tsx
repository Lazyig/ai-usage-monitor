import { ArrowReset20Regular } from '@fluentui/react-icons/headless/svg/arrow-reset'
import { ArrowSync20Regular } from '@fluentui/react-icons/headless/svg/arrow-sync'
import { Delete20Regular } from '@fluentui/react-icons/headless/svg/delete'
import { Desktop20Regular } from '@fluentui/react-icons/headless/svg/desktop'
import { Edit20Regular } from '@fluentui/react-icons/headless/svg/edit'
import { KeyReset20Regular } from '@fluentui/react-icons/headless/svg/key-reset'
import { MoreHorizontal20Regular } from '@fluentui/react-icons/headless/svg/more-horizontal'
import { Prohibited20Regular } from '@fluentui/react-icons/headless/svg/prohibited'
import { useMemo, useState, type FormEvent } from 'react'
import { createDevice, renameDevice, deleteDevice, getDevices, purgeDevice, purgeDeviceEvents, requestResync, rotateDevice } from '../api/client'
import type { Device } from '../api/types'
import { Badge } from '../components/Badge'
import { Card } from '../components/Card'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { type Column, DataTable } from '../components/DataTable'
import { PromptDialog } from '../components/PromptDialog'
import { RowField } from '../components/RowField'
import { StateContainer } from '../components/StateContainer'
import { countColumn, dateTimeColumn } from '../components/columns'
import { Button } from '../components/ui/button'
import { Checkbox } from '../components/ui/checkbox'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '../components/ui/dropdown-menu'
import { Input } from '../components/ui/input'
import { Spinner } from '../components/ui/spinner'
import { Tooltip } from '../components/ui/tooltip'
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
      media: () => <Desktop20Regular />,
      render: (d) => (
        <span>
          {d.name} {!d.revoked_at && isStale(d.last_seen) && <Badge kind="stale" />}{' '}
          {d.revoked_at && <Badge kind="revoked" title={`폐기 시각: ${formatDateTime(d.revoked_at)}`} />}{' '}
          {(rotating === d.id || revoking === d.id) && (
            <Spinner size="extra-tiny" label={rotating === d.id ? '재발급 중…' : '폐기 중…'} className="inline-flex align-middle" />
          )}
        </span>
      ),
      // The agent-reported hostname, when the operator renamed the device.
      description: (d) =>
        d.hostname && d.hostname !== d.name ? <span title="에이전트가 보고한 호스트명">{d.hostname}</span> : undefined,
      sortValue: (d) => d.name,
      // Row actions, revealed on hover/focus: rename, then the overflow
      // menu with destructive items last and in red.
      actions: (d) => {
        const busy = rotating === d.id || revoking === d.id
        return (
          <>
            <Tooltip content="이름 변경">
              <Button
                appearance="subtle"
                icon={<Edit20Regular />}
                aria-label={`"${d.name}" 이름 변경`}
                onClick={() => setConfirmState({ kind: 'rename', device: d })}
              />
            </Tooltip>
            <DropdownMenu>
              <Tooltip content={busy ? (rotating === d.id ? '재발급 중…' : '폐기 중…') : '작업'}>
                <DropdownMenuTrigger asChild>
                  <Button
                    appearance="subtle"
                    icon={busy ? <Spinner size="tiny" /> : <MoreHorizontal20Regular />}
                    disabled={busy}
                    aria-label={`"${d.name}" 작업`}
                  />
                </DropdownMenuTrigger>
              </Tooltip>
              <DropdownMenuContent side="bottom" align="end">
                <DropdownMenuItem icon={<KeyReset20Regular />} onSelect={() => setConfirmState({ kind: 'rotate', device: d })}>
                  토큰 재발급
                </DropdownMenuItem>
                <DropdownMenuItem
                  icon={<ArrowSync20Regular />}
                  disabled={!!d.revoked_at}
                  title={d.revoked_at ? '폐기된 기기는 재동기화할 수 없습니다. 먼저 토큰을 재발급하세요.' : '삭제 없이 로그를 처음부터 다시 보내게 합니다'}
                  onSelect={() => setConfirmState({ kind: 'resync', device: d })}
                >
                  재동기화
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  danger
                  icon={<ArrowReset20Regular />}
                  disabled={!!d.revoked_at}
                  title={d.revoked_at ? '폐기된 기기는 초기화할 수 없습니다. 먼저 토큰을 재발급하세요.' : '이 기기의 이벤트를 모두 지운 뒤 다시 받습니다'}
                  onSelect={() => setConfirmState({ kind: 'reset', device: d })}
                >
                  초기화
                </DropdownMenuItem>
                <DropdownMenuItem danger icon={<Prohibited20Regular />} disabled={!!d.revoked_at} onSelect={() => setConfirmState({ kind: 'revoke', device: d })}>
                  {d.revoked_at ? '폐기됨' : '토큰 폐기'}
                </DropdownMenuItem>
                <DropdownMenuItem danger icon={<Delete20Regular />} onSelect={() => setConfirmState({ kind: 'purge', device: d })}>
                  완전 삭제
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </>
        )
      },
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
  ]

  return (
    <div>
      <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
        <h1>기기</h1>
      </div>

      <ul className="mx-0 mt-0 mb-4 flex list-none flex-col gap-0.75 p-0 text-200 text-fg2 [&_strong]:text-fg1">
        <li>
          <strong>토큰 재발급</strong> — 기존 토큰을 즉시 무효화하고 새 토큰을 발급합니다. 기기 이력은 그대로 유지되며, 폐기된 기기도 다시 살아납니다.
        </li>
        <li>
          <strong>재동기화</strong> — 아무것도 지우지 않고, 에이전트가 다음 하트비트(1분 이내)에 로그를 처음부터 다시 보내도록 합니다. 이미 있는 이벤트는 무시되고 빠진 것만 채워집니다.
        </li>
        <li>
          <strong>초기화</strong> — 이 기기의 이벤트를 모두 지운 뒤 에이전트가 다시 보내도록 합니다. 에이전트 PC에서 이미 지워진 로그(예: Claude Code는 오래된 대화 기록을 자동 삭제)의 사용량은 되돌릴 수 없으니, 보통은 재동기화를 쓰세요.
        </li>
        <li>
          <strong>토큰 폐기</strong> — 토큰만 무효화합니다. 이벤트 기록은 그대로 유지됩니다.
        </li>
        <li>
          <strong>완전 삭제</strong> — 기기와 이 기기의 모든 이벤트를 영구적으로 삭제합니다.
        </li>
      </ul>

      <Card className="mb-6" title="기기 추가">
        <form className="mb-3 flex flex-wrap items-end gap-2 max-phone:flex-col max-phone:items-stretch" onSubmit={onCreate}>
          <RowField label="이름">
            <Input id="device-name" value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="예: macbook-pro" />
          </RowField>
          <Button touch appearance="primary" type="submit" disabled={creating || !newName.trim()}>
            {creating ? '등록 중…' : '추가'}
          </Button>
        </form>
        {createError && <p className="text-200 text-danger">{createError}</p>}
      </Card>

      {newToken &&
        (() => {
          const cmds = installCommands(origin, newToken.token)
          return (
            <div className="mx-0 my-3 rounded-md border border-warning-stroke bg-warning-bg p-3 [&_code]:min-w-0 [&_code]:flex-1 [&_code]:overflow-x-auto [&_code]:rounded-md [&_code]:border [&_code]:border-stroke2 [&_code]:bg-bg1 [&_code]:px-2.5 [&_code]:py-2 [&_code]:font-mono [&_code]:text-200 [&_code]:whitespace-pre">
              <strong>{newToken.name}</strong>의 토큰이 발급되었습니다. 이 토큰은 다시 표시되지 않으니 지금 복사하세요.
              <div className="mt-1.5 flex items-center gap-2 [&_[data-slot=button]]:shrink-0">
                <code>{newToken.token}</code>
                <Button touch size="small" onClick={() => navigator.clipboard?.writeText(newToken.token)}>
                  복사
                </Button>
              </div>
              <p style={{ marginTop: 10, marginBottom: 2 }}>Linux / macOS 설치 명령</p>
              <div className="mt-1.5 flex items-center gap-2 [&_[data-slot=button]]:shrink-0">
                <code>{cmds.unix}</code>
                <Button touch size="small" onClick={() => navigator.clipboard?.writeText(cmds.unix)}>
                  복사
                </Button>
              </div>
              <p style={{ marginTop: 10, marginBottom: 2 }}>Windows (PowerShell) 설치 명령</p>
              <p className="mx-0 mt-1.5 mb-2 rounded-md border border-warning-stroke bg-warning-bg px-3 py-2 text-200 leading-200 [&_strong]:text-warning-fg" role="note">
                <strong>관리자 권한 필수</strong> — PowerShell 을 &quot;관리자 권한으로 실행&quot; 한 창에서 실행하세요. 일반 권한에서는 Windows 서비스 등록이 실패해
                에이전트가 자동 시작되지 않습니다.
              </p>
              <div className="mt-1.5 flex items-center gap-2 [&_[data-slot=button]]:shrink-0">
                <code>{cmds.windows}</code>
                <Button touch size="small" onClick={() => navigator.clipboard?.writeText(cmds.windows)}>
                  복사
                </Button>
              </div>
            </div>
          )
        })()}

      {actionNotice && <p className="mx-0 my-2 rounded-md border border-success-stroke bg-success-bg px-3 py-2 text-300 text-fg1">{actionNotice}</p>}
      {actionError && (
        <p className="text-200 text-danger" role="alert">
          작업에 실패했습니다: {actionError}
        </p>
      )}

      <Checkbox
        className="mb-3"
        checked={showRevoked}
        onCheckedChange={(checked) => setShowRevoked(checked === true)}
        label={`폐기된 기기 표시${revokedCount > 0 ? ` (${revokedCount})` : ''}`}
      />

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
          message={`"${confirmState.device.name}" 기기의 이벤트를 모두 삭제합니다. 이 기기의 통계가 잠시 0 이 되었다가 에이전트가 1분 안에 다시 보냅니다. 에이전트 PC에서 이미 지워진 로그의 사용량은 돌아오지 않습니다.`}
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
