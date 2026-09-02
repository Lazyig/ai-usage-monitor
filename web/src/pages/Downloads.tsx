import { useEffect, useState } from 'react'
import { ApiError, getDownloadsManifest } from '../api/client'
import type { DownloadFile, DownloadsManifest } from '../api/types'
import { type Column, DataTable } from '../components/DataTable'
import { StateContainer } from '../components/StateContainer'
import { describeError } from '../lib/errors'
import { formatBytes, formatDateTime } from '../lib/format'
import { installCommands } from '../lib/installCommands'
import { useFilters } from '../state/FiltersContext'

const TOKEN_PLACEHOLDER = '<기기 페이지에서 발급받은 토큰>'

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

  const columns: Column<DownloadFile>[] = [
    { key: 'name', header: '파일', render: (f) => <span className="mono">{f.name}</span>, sortValue: (f) => f.name },
    { key: 'os', header: 'OS', render: (f) => f.os, sortValue: (f) => f.os },
    { key: 'arch', header: 'Arch', render: (f) => f.arch, sortValue: (f) => f.arch },
    { key: 'size', header: '크기', align: 'right', render: (f) => formatBytes(f.size), sortValue: (f) => f.size },
    {
      key: 'sha256',
      header: 'SHA256',
      render: (f) => (
        <span className="mono" title={f.sha256}>
          {f.sha256.slice(0, 12)}…
        </span>
      ),
    },
    {
      key: 'download',
      header: '',
      render: (f) => (
        <a className="btn btn-sm" href={`/downloads/${encodeURIComponent(f.name)}`}>
          다운로드
        </a>
      ),
    },
  ]

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
        <StateContainer
          loading={state.status === 'loading'}
          error={state.status === 'error' ? state.message : null}
          empty={state.status === 'success' && state.data.files.length === 0}
          emptyMessage="아직 빌드된 파일이 없습니다."
        >
          {state.status === 'success' && (
            <>
              <p>
                버전 <strong>{state.data.version}</strong> · 빌드 시각 {formatDateTime(state.data.built_at)}
              </p>
              <DataTable columns={columns} rows={state.data.files} rowKey={(f) => f.name} initialSortKey="name" initialSortDir="asc" />
              <InstallSnippets />
            </>
          )}
        </StateContainer>
      )}
    </div>
  )
}

/** Copy-paste install one-liners for both platforms, using a placeholder
 * token since this page (unlike the Devices page right after issuance)
 * isn't tied to any one device. */
function InstallSnippets() {
  const origin = typeof window !== 'undefined' ? window.location.origin : ''
  const cmds = installCommands(origin, TOKEN_PLACEHOLDER)
  return (
    <div className="card section" style={{ marginTop: 16 }}>
      <h2>설치</h2>
      <p>기기 페이지에서 토큰을 발급한 뒤 아래 명령의 {TOKEN_PLACEHOLDER} 를 실제 토큰으로 바꿔 실행하세요.</p>
      <p style={{ marginTop: 10, marginBottom: 2 }}>Linux / macOS</p>
      <div className="copy-box-row">
        <code>{cmds.unix}</code>
        <button type="button" className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(cmds.unix)}>
          복사
        </button>
      </div>
      <p style={{ marginTop: 10, marginBottom: 2 }}>Windows (PowerShell)</p>
      <div className="copy-box-row">
        <code>{cmds.windows}</code>
        <button type="button" className="btn btn-sm" onClick={() => navigator.clipboard?.writeText(cmds.windows)}>
          복사
        </button>
      </div>
    </div>
  )
}
