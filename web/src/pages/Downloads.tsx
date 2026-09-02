import { useEffect, useState } from 'react'
import { ApiError, getDownloadsManifest } from '../api/client'
import type { DownloadFile, DownloadsManifest } from '../api/types'
import { OsBadge, osLabel } from '../components/Badge'
import { Card } from '../components/Card'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { describeError } from '../lib/errors'
import { formatBytes, formatDateTime } from '../lib/format'
import { installCommands, uninstallCommands } from '../lib/installCommands'
import { useFilters } from '../state/FiltersContext'

const TOKEN_PLACEHOLDER = '<기기 관리에서 발급받은 토큰>'

type LoadState =
  | { status: 'loading' }
  | { status: 'not-built' }
  | { status: 'error'; message: string }
  | { status: 'success'; data: DownloadsManifest }

export function Downloads() {
  const { refreshKey } = useFilters()
  const [state, setState] = useState<LoadState>({ status: 'loading' })

  useEffect(() => {
    let cancelled = false
    setState({ status: 'loading' })
    getDownloadsManifest()
      .then((data) => {
        if (!cancelled) setState({ status: 'success', data })
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (err instanceof ApiError && err.status === 404) {
          setState({ status: 'not-built' })
        } else {
          setState({ status: 'error', message: describeError(err) })
        }
      })
    return () => {
      cancelled = true
    }
  }, [refreshKey])

  return (
    <div>
      <div className="page-header">
        <h1>다운로드</h1>
      </div>

      {state.status === 'not-built' ? (
        <div className="state-panel state-empty">
          <p>아직 빌드된 에이전트가 없습니다. CI가 첫 릴리스를 만들면 여기에 표시됩니다.</p>
        </div>
      ) : (
        <Card
          className="section"
          title="에이전트 다운로드"
          controls={
            state.status === 'success' ? (
              <span className="card-meta">
                버전 {state.data.version} · 빌드 {formatDateTime(state.data.built_at)}
              </span>
            ) : undefined
          }
        >
          <StateContainer
            loading={state.status === 'loading'}
            error={state.status === 'error' ? state.message : null}
            empty={state.status === 'success' && state.data.files.length === 0}
            emptyMessage="아직 빌드된 파일이 없습니다."
          >
            {state.status === 'success' && <DownloadsTable files={state.data.files} />}
          </StateContainer>
        </Card>
      )}

      {state.status === 'success' && <InstallGuideCard />}
      {state.status === 'success' && <UninstallGuideCard />}
    </div>
  )
}

function DownloadsTable({ files }: { files: DownloadFile[] }) {
  const columns: Column<DownloadFile>[] = [
    {
      key: 'os',
      header: 'OS',
      render: (f) => <OsBadge os={f.os} />,
      sortValue: (f) => `${osLabel(f.os)}_${f.arch}`,
    },
    { key: 'arch', header: 'Arch', render: (f) => f.arch, sortValue: (f) => f.arch },
    { key: 'name', header: '파일', render: (f) => <span className="mono">{f.name}</span>, sortValue: (f) => f.name },
    { key: 'size', header: '크기', align: 'right', render: (f) => formatBytes(f.size), sortValue: (f) => f.size },
    {
      key: 'sha256',
      header: 'SHA256',
      render: (f) => (
        <span className="sha-cell">
          <span className="mono" title={f.sha256}>
            {f.sha256.slice(0, 10)}…
          </span>
          <button
            type="button"
            className="btn btn-sm btn-ghost"
            onClick={() => navigator.clipboard?.writeText(f.sha256)}
            title="SHA256 전체 복사"
          >
            복사
          </button>
        </span>
      ),
    },
    {
      key: 'download',
      header: '',
      render: (f) => (
        <a className="btn btn-sm btn-secondary" href={`/downloads/${encodeURIComponent(f.name)}`}>
          <span aria-hidden="true">↓</span> 다운로드
        </a>
      ),
    },
  ]
  return <DataTable columns={columns} rows={files} rowKey={(f) => f.name} initialSortKey="os" initialSortDir="asc" />
}

/** Splits a command string around a placeholder substring so the
 * placeholder can be highlighted in a distinct color; the rest of the
 * command renders as plain monospace text. */
function CommandBox({ command, highlight = '' }: { command: string; highlight?: string }) {
  const idx = highlight ? command.indexOf(highlight) : -1
  const before = idx >= 0 ? command.slice(0, idx) : command
  const after = idx >= 0 ? command.slice(idx + highlight.length) : ''
  return (
    <div className="command-box-wrap">
      <pre className="command-box">
        {before}
        {idx >= 0 && <span className="token-placeholder">{highlight}</span>}
        {after}
      </pre>
      <button
        type="button"
        className="btn btn-sm command-box-copy"
        onClick={() => navigator.clipboard?.writeText(command)}
        aria-label="명령 복사"
      >
        복사
      </button>
    </div>
  )
}

/** Copy-paste install guide for both platforms, using a placeholder token
 * since this page (unlike the Devices page right after issuance) isn't tied
 * to any one device. */
function InstallGuideCard() {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const cmds = installCommands(origin, TOKEN_PLACEHOLDER)
  return (
    <Card
      className="section"
      title="에이전트 설치"
      subtitle="아래 명령으로 에이전트를 설치하면 사용량을 자동으로 수집해 이 대시보드에 보고합니다."
    >
      <div className="install-tabs">
        <div className="install-panel">
          <h3>Linux / macOS</h3>
          <CommandBox command={cmds.unix} highlight={TOKEN_PLACEHOLDER} />
          <ol className="install-steps">
            <li>기기 관리에서 기기 추가 → 토큰 발급</li>
            <li>아래 명령을 실행</li>
            <li>1분 안에 기기 관리 목록에 나타남</li>
          </ol>
        </div>
        <div className="install-panel">
          <h3>Windows (PowerShell)</h3>
          <CommandBox command={cmds.windows} highlight={TOKEN_PLACEHOLDER} />
          <ol className="install-steps">
            <li>기기 관리에서 기기 추가 → 토큰 발급</li>
            <li>아래 명령을 실행</li>
            <li>1분 안에 기기 관리 목록에 나타남</li>
          </ol>
          <p className="install-note">관리자 PowerShell 에서 실행하세요.</p>
        </div>
      </div>
      <p className="install-note">
        <span className="token-placeholder">{TOKEN_PLACEHOLDER}</span> 자리에는 실제 토큰이 들어갑니다 — 기기 관리에서 기기를 추가하면
        토큰이 포함된 실제 명령이 표시됩니다.
      </p>
    </Card>
  )
}

function UninstallGuideCard() {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const cmds = uninstallCommands(origin)
  return (
    <Card
      className="section"
      title="에이전트 제거"
      subtitle="서비스 등록을 해제하고 바이너리와 로컬 상태(설정·커서·오프라인 큐)를 지웁니다."
    >
      <div className="install-tabs">
        <div className="install-panel">
          <h3>Linux / macOS</h3>
          <CommandBox command={cmds.unix} />
          <ol className="install-steps">
            <li>systemd(user) 또는 launchd 등록 해제</li>
            <li>바이너리(/usr/local/bin 또는 ~/.local/bin) 삭제</li>
            <li>~/.config/ai-usage-agent (macOS 는 ~/Library/Application Support) 삭제</li>
          </ol>
          <p className="install-note">
            로컬 상태를 남기려면 <code>| sh -s -- --keep-state</code> 로 실행하세요.
          </p>
        </div>
        <div className="install-panel">
          <h3>Windows (PowerShell)</h3>
          <CommandBox command={cmds.windows} />
          <ol className="install-steps">
            <li>ai-usage-agent 서비스 중지 및 삭제</li>
            <li>%LOCALAPPDATA%\ai-usage-agent 삭제</li>
            <li>%APPDATA%\ai-usage-agent (설정·커서·큐) 삭제</li>
          </ol>
          <p className="install-note">
            관리자 PowerShell 에서 실행하세요. 로컬 상태를 남기려면 명령 끝에 <code> -KeepState</code> 를 붙이세요.
          </p>
        </div>
      </div>
      <p className="install-note">
        제거 후에도 서버는 그 기기의 토큰을 계속 받아들이므로, 기기 관리에서 <strong>토큰 폐기</strong>(이력 유지) 또는{' '}
        <strong>완전 삭제</strong>(이력까지 삭제)로 마무리하세요. 같은 기기를 다시 설치하면 이벤트 ID 가 로그 기준이라 중복 집계는 생기지
        않습니다.
      </p>
    </Card>
  )
}
