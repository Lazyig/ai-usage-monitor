import { getServerBuild } from '../api/client'
import { useApiData } from '../hooks/useApiData'
import { formatDateTime } from '../lib/format'
import { useRefreshKey } from '../state/FiltersContext'

/** The running server's release and commit, under the navigation: what to
 * quote when reporting a problem, and how to tell a deploy went out. Shows
 * nothing until it is known (and on an older server without the endpoint). */
export function ServerBuild() {
  // The navigation stays mounted across pages, so follow the refresh button and
  // auto refresh: a deploy shows up without reloading the page.
  const refreshKey = useRefreshKey()
  const state = useApiData(() => getServerBuild(), [refreshKey])
  if (state.status !== 'success') return null
  const b = state.data
  const short = b.commit ? b.commit.slice(0, 7) : ''
  const title = [
    `버전 ${b.version}`,
    b.commit && `커밋 ${b.commit}${b.modified ? ' (커밋되지 않은 변경 포함)' : ''}`,
    b.commit_time && `커밋 시각 ${formatDateTime(b.commit_time)}`,
    b.go_version,
  ]
    .filter(Boolean)
    .join('\n')
  return (
    <p className="m-0 w-full truncate px-2.5 pt-1 pb-1.5 text-100 text-fg3" title={title} data-testid="server-build">
      서버 {b.version}
      {short && (
        <>
          {' · '}
          <span className="font-mono">{short}</span>
          {b.modified && '*'}
        </>
      )}
    </p>
  )
}
