import type { Device } from '../../api/types'
import { updateCommands } from '../../lib/installCommands'
import { CopyableCode } from '../CopyableCode'

/** Lists the devices whose agent differs from the published build, with the
 * update one-liners. The update keeps config.json, so no token is involved. */
export function UpdateNotice({ origin, published, devices }: { origin: string; published: string; devices: Device[] }) {
  const cmds = updateCommands(origin)
  return (
    <div role="status" className="mx-0 mt-0 mb-4 rounded-md border border-warning-stroke bg-warning-bg p-3 text-300 text-fg1">
      <strong>
        배포 중인 버전({published})과 다른 에이전트가 {devices.length}대 있습니다.
      </strong>
      <ul className="mx-0 my-1.5 list-disc pl-[18px] text-200 text-fg2">
        {devices.map((d) => (
          <li key={d.id}>
            {d.name} — {d.agent_version}
          </li>
        ))}
      </ul>
      <p className="m-0 text-200 text-fg2">
        해당 기기에서 아래 명령을 실행하면 서버 주소·토큰·수집 폴더 설정을 그대로 둔 채 업데이트되고, 1분 안에 새 버전이 보고됩니다. 처음 설치한 계정으로
        실행하세요(root 로 설치했다면 <strong>sudo sh</strong>, Windows 는 관리자 PowerShell).
      </p>
      <p className="mt-2.5 mb-0.5">Linux / macOS</p>
      <CopyableCode value={cmds.unix} ariaLabel="Linux / macOS 업데이트 명령 복사" />
      <p className="mt-2.5 mb-0.5">Windows (관리자 PowerShell)</p>
      <CopyableCode value={cmds.windows} ariaLabel="Windows 업데이트 명령 복사" />
    </div>
  )
}
