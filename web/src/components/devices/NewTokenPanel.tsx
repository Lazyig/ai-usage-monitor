import { installCommands } from '../../lib/installCommands'
import { CopyableCode } from '../CopyableCode'
import { Notice } from '../Notice'

/** Shown once after a token is issued or rotated: the token itself and the
 * install one-liners that carry it. The server never returns it again. */
export function NewTokenPanel({ origin, name, token }: { origin: string; name: string; token: string }) {
  const cmds = installCommands(origin, token)
  return (
    <div className="mx-0 my-3 rounded-md border border-warning-stroke bg-warning-bg p-3">
      <strong>{name}</strong>의 토큰이 발급되었습니다. 이 토큰은 다시 표시되지 않으니 지금 복사하세요.
      <CopyableCode value={token} />
      <p className="mt-2.5 mb-0.5">Linux / macOS 설치 명령</p>
      <CopyableCode value={cmds.unix} />
      <p className="mt-2.5 mb-0.5">Windows (PowerShell) 설치 명령</p>
      <Notice>
        <strong>관리자 권한 필수</strong> — PowerShell 을 &quot;관리자 권한으로 실행&quot; 한 창에서 실행하세요. 일반 권한에서는 Windows 서비스 등록이 실패해
        에이전트가 자동 시작되지 않습니다.
      </Notice>
      <CopyableCode value={cmds.windows} />
    </div>
  )
}
