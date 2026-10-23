import { useEffect, useState } from 'react'
import { ApiError, getDownloadsManifest } from '../api/client'
import type { DownloadFile, DownloadsManifest } from '../api/types'
import { OsBadge, osLabel } from '../components/Badge'
import { Card } from '../components/Card'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { Button } from '../components/ui/button'
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
      <div className="mt-2 mb-5 flex flex-wrap items-center justify-between gap-3 [&>h1]:m-0">
        <h1>다운로드</h1>
      </div>

      {state.status === 'not-built' ? (
        <div className="flex items-center justify-center rounded-lg border border-dashed border-stroke2 px-4 py-8 text-center text-fg3 [&_p]:m-0 [&_p]:text-inherit">
          <p>아직 빌드된 에이전트가 없습니다. CI가 첫 릴리스를 만들면 여기에 표시됩니다.</p>
        </div>
      ) : (
        <Card
          className="mb-6"
          title="에이전트 다운로드"
          controls={
            state.status === 'success' ? (
              <span className="text-200 whitespace-nowrap text-fg3">
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
    { key: 'name', header: '파일', render: (f) => <span className="font-mono">{f.name}</span>, sortValue: (f) => f.name },
    { key: 'size', header: '크기', align: 'right', render: (f) => formatBytes(f.size), sortValue: (f) => f.size },
    {
      key: 'sha256',
      header: 'SHA256',
      render: (f) => (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-mono" title={f.sha256}>
            {f.sha256.slice(0, 10)}…
          </span>
          <Button touch size="small" appearance="subtle"
            onClick={() => navigator.clipboard?.writeText(f.sha256)}
            title="SHA256 전체 복사"
          >
            복사
          </Button>
        </span>
      ),
    },
    {
      key: 'download',
      header: '',
      render: (f) => (
        <Button touch size="small" asChild>
          <a href={`/downloads/${encodeURIComponent(f.name)}`}>
            <span aria-hidden="true">↓</span> 다운로드
          </a>
        </Button>
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
    <div className="relative mb-1">
      <pre className="m-0 max-w-full overflow-x-auto rounded-md border border-code-stroke bg-code-bg py-2.5 pr-11 pl-3 font-mono text-200 whitespace-pre text-code-fg">
        {before}
        {idx >= 0 && <span className="font-mono font-semibold text-series-4">{highlight}</span>}
        {after}
      </pre>
      <Button touch size="small" className="absolute top-1.5 right-1.5 shadow-[-10px_0_10px_var(--code-bg)]"
        onClick={() => navigator.clipboard?.writeText(command)}
        aria-label="명령 복사"
      >
        복사
      </Button>
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
      className="mb-6"
      title="에이전트 설치"
      subtitle="아래 명령으로 에이전트를 설치하면 사용량을 자동으로 수집해 이 대시보드에 보고합니다."
    >
      <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-5 *:min-w-0 max-narrow:grid-cols-[1fr]">
        <div>
          <h3>Linux / macOS</h3>
          <CommandBox command={cmds.unix} highlight={TOKEN_PLACEHOLDER} />
          <ol className="mx-0 mt-1 mb-0 list-decimal pl-[18px] text-200 text-fg2 [&_li]:mb-[3px]">
            <li>기기 관리에서 기기 추가 → 토큰 발급</li>
            <li>아래 명령을 실행</li>
            <li>1분 안에 기기 관리 목록에 나타남</li>
          </ol>
        </div>
        <div>
          <h3>Windows (PowerShell)</h3>
          <p className="mx-0 mt-1.5 mb-2 rounded-md border border-warning-stroke bg-warning-bg px-3 py-2 text-200 leading-200 [&_strong]:text-warning-fg" role="note">
            <strong>관리자 권한 필수</strong> — 시작 메뉴에서 PowerShell 을 &quot;관리자 권한으로 실행&quot; 한 뒤 아래 명령을 붙여 넣으세요. 일반 권한에서는
            서비스 등록이 실패합니다.
          </p>
          <CommandBox command={cmds.windows} highlight={TOKEN_PLACEHOLDER} />
          <ol className="mx-0 mt-1 mb-0 list-decimal pl-[18px] text-200 text-fg2 [&_li]:mb-[3px]">
            <li>기기 관리에서 기기 추가 → 토큰 발급</li>
            <li>관리자 PowerShell 에서 위 명령을 실행</li>
            <li>1분 안에 기기 관리 목록에 나타남</li>
          </ol>
        </div>
      </div>
      <p className="mt-2 text-200 text-fg3">
        <span className="font-mono font-semibold text-series-4">{TOKEN_PLACEHOLDER}</span> 자리에는 실제 토큰이 들어갑니다 — 기기 관리에서 기기를 추가하면
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
      className="mb-6"
      title="에이전트 제거"
      subtitle="서비스 등록을 해제하고 바이너리와 로컬 상태(설정·커서·오프라인 큐)를 지웁니다."
    >
      <div className="grid grid-cols-[repeat(2,minmax(0,1fr))] gap-5 *:min-w-0 max-narrow:grid-cols-[1fr]">
        <div>
          <h3>Linux / macOS</h3>
          <CommandBox command={cmds.unix} />
          <ol className="mx-0 mt-1 mb-0 list-decimal pl-[18px] text-200 text-fg2 [&_li]:mb-[3px]">
            <li>systemd(user) 또는 launchd 등록 해제</li>
            <li>바이너리(/usr/local/bin 또는 ~/.local/bin) 삭제</li>
            <li>~/.config/ai-usage-agent (macOS 는 ~/Library/Application Support) 삭제</li>
          </ol>
          <p className="mt-2 text-200 text-fg3">
            로컬 상태를 남기려면 <code>| sh -s -- --keep-state</code> 로 실행하세요.
          </p>
        </div>
        <div>
          <h3>Windows (PowerShell)</h3>
          <CommandBox command={cmds.windows} />
          <ol className="mx-0 mt-1 mb-0 list-decimal pl-[18px] text-200 text-fg2 [&_li]:mb-[3px]">
            <li>ai-usage-agent 서비스 중지 및 삭제</li>
            <li>%LOCALAPPDATA%\ai-usage-agent 삭제</li>
            <li>%APPDATA%\ai-usage-agent (설정·커서·큐) 삭제</li>
          </ol>
          <p className="mx-0 mt-1.5 mb-2 rounded-md border border-warning-stroke bg-warning-bg px-3 py-2 text-200 leading-200 [&_strong]:text-warning-fg" role="note">
            <strong>관리자 권한 필수</strong> — 서비스 삭제에 필요합니다. 로컬 상태를 남기려면 명령 끝에 <code> -KeepState</code> 를 붙이세요.
          </p>
        </div>
      </div>
      <p className="mt-2 text-200 text-fg3">
        제거 후에도 서버는 그 기기의 토큰을 계속 받아들이므로, 기기 관리에서 <strong>토큰 폐기</strong>(이력 유지) 또는{' '}
        <strong>완전 삭제</strong>(이력까지 삭제)로 마무리하세요. 같은 기기를 다시 설치하면 이벤트 ID 가 로그 기준이라 중복 집계는 생기지
        않습니다.
      </p>
    </Card>
  )
}
