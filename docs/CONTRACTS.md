# 패키지 계약 (병렬 작업용)

여러 작업자가 동시에 구현하므로 아래 시그니처와 디렉터리 소유권을 지킨다. 바꿔야 하면 이 문서를 먼저 고친다.

## 디렉터리 소유권

| 경로 | 소유 작업 | 비고 |
|---|---|---|
| `internal/event`, `internal/collector/collector.go`, `internal/pricing` | 공통 (이미 있음) | 수정 시 다른 작업자와 조율 |
| `internal/collector/claudecode`, `internal/collector/codex` | collectors | |
| `internal/store`, `internal/httpapi`, `cmd/server` | server | `docs/API.md` 구현 |
| `web/` | spa | `docs/API.md` 소비 |
| `internal/cursor`, `internal/transport`, `internal/agentcfg`, `cmd/agent` | agent-runtime | 수집기 3종을 조립 |
| `.gitlab-ci.yml`, `deploy/`, `cmd/server/kodata` | ci (메인) | |

## 수집기 생성자

모든 수집기는 `collector.Collector` 를 구현하고 아래 생성자를 노출한다. `root` 는 OS 별 기본 경로를 agent-runtime 이 넣어준다.

```go
// internal/collector/claudecode
func New(root string) collector.Collector   // root = ~/.claude/projects
// internal/collector/codex
func New(root string) collector.Collector   // root = ~/.codex/sessions
// internal/collector/antigravity
func New(root string) collector.Collector   // root = ~/.gemini/antigravity-cli
```

규칙
- `Scan(ctx, deviceID, cursor)` 는 **재실행해도 안전**해야 한다 (중복 이벤트는 서버가 무시, 누락은 안 됨).
- cursor 는 수집기 내부 JSON. nil 이면 처음부터. 파일이 줄어들었거나 inode 가 바뀌면 그 파일은 처음부터 다시 읽는다.
  **기기 ID 는 넣지 않는다**: native key 만으로 전역 유일하고, 기기 ID 는 재등록·whoami 실패로 바뀔 수 있어
  섞으면 같은 로그가 새 ID 로 재유입돼 이중 계산된다. `Scan` 의 `deviceID` 인자는 `UsageEvent.DeviceID` 채우는 데만 쓴다.
- `Timestamp` 는 UTC. `Input` 은 캐시 제외(Codex 는 `input - cached` 로 변환).
- root 가 없으면 에러가 아니라 빈 결과 (도구 미설치 기기).
- 원본 텍스트(프롬프트·응답)는 절대 이벤트에 넣지 않는다.
- 단위 테스트는 `testdata/` 의 **익명화된** 샘플로. 실제 홈 디렉터리를 읽는 테스트는 파일이 없으면 `t.Skip`.

## 서버 ↔ 에이전트

- `GET /api/v1/agent/whoami` (Bearer 기기 토큰) → `{"device_id","name"}`. 에이전트는 시작 시 이걸로 기기 ID 를 맞춘다 (토큰 = 기기 식별자).
- `POST /api/v1/ingest` (Bearer 기기 토큰) body: `{"device":{"id","name","os","arch","agent_version"},"events":[UsageEvent...]}` → `{"accepted":n,"duplicates":n,"rejected":n,"device_id"}`. gzip 본문 허용.
  응답에 `"resync":true` 가 붙으면(딱 한 번) 서버가 그 기기 데이터를 지웠다는 뜻 — 에이전트는 커서와 오프라인 큐를 비우고 처음부터 다시 수집한다.
- `GET /api/v1/agent/latest` (토큰 불필요) → 다운로드 가능한 에이전트 버전/체크섬 (자동 업데이트용, 2차)

## 서버 ↔ SPA

`docs/API.md` 참조. SPA 는 `/api/*` 만 호출하고 정적 파일은 서버가 `/` 에서 서빙한다.

## 빌드 산출물 위치

- 에이전트 바이너리: CI 가 `cmd/server/kodata/downloads/<name>` 에 복사 + `manifest.json` 생성. 서버는 `$KO_DATA_PATH/downloads` 를 `/downloads/` 로 서빙 (로컬 개발은 `./cmd/server/kodata/downloads`).
- SPA: `web/dist` 를 `cmd/server/web_embed.go` 가 `//go:embed` 로 포함. dist 가 없으면 빈 페이지 대신 "SPA not built" 안내.
