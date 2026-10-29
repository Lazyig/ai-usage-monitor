# HTTP API v1

## 공통

- 경로는 `/api/v1` 아래. 인증은 `Authorization: Bearer <token>`.
  - 기기 토큰: `POST /ingest`, `GET /agent/whoami` 만 허용
  - 관리자 토큰(`ADMIN_TOKEN` 환경변수): 나머지 관리·조회 API 전부. 미설정이면 `503 {"error":"admin token not configured"}`
- 실패: `401 {"error":"unauthorized"}`. 그 외 오류는 `{"error":"<message>"}` 와 4xx/5xx.
- 시각은 RFC3339 UTC. 금액은 USD float. 토큰 수는 정수.
- 기간 파라미터: `from`, `to` (`to` 는 exclusive). RFC3339 는 절대 시각, `YYYY-MM-DD` 는 `tz` 기준 자정. 기본 구간은 `to` = 지금, `from` = `to` 가 속한 날짜의 자정에서 29일 전(로컬 기준 최근 30일).
- `tz` (IANA, 기본 `Asia/Seoul`) 는 날짜 해석과 일·시 버킷 경계에만 쓴다.
- 모든 `/api/*` 응답에 `Cache-Control: no-store`. 요청 본문은 8 MiB 까지(초과 시 `413`).

## 에이전트용 (기기 토큰)

### GET /agent/whoami
→ `{"device_id":"<uuid>","name":"macbook"}`. 토큰이 곧 기기 식별자이며, 에이전트는 시작할 때 이 값으로 자기 기기 ID 를 맞춘다.

### POST /ingest
```json
{"device":{"id":"<uuid>","name":"macbook","os":"darwin","arch":"arm64","agent_version":"0.1.0"},
 "events":[{ ...UsageEvent JSON (internal/event) ... }]}
```
→ `200 {"accepted":12,"duplicates":3,"updated":1,"rejected":0,"device_id":"<uuid>"}`. `updated` 는 0 이면 생략된다.

- `Content-Encoding: gzip` 본문 허용. 압축을 푼 본문은 16 MiB 까지(초과 시 `413`). 배치 최대 5,000건(초과 시 `400 {"error":"batch too large: at most 5000 events"}`, 5,001번째 이벤트에서 읽기를 멈춘다). 빈 배치는 하트비트로 쓰인다(유휴 에이전트는 5분마다).
- `device.id` 는 참고용이다. 이벤트는 항상 토큰에 묶인 기기로 저장되고, 응답의 `device_id` 가 에이전트가 앞으로 쓸 기기 ID 를 알려준다.
- `rejected` 는 `event.Valid()` 실패 또는 `device_id` 불일치로 버려진 이벤트 수. 나머지는 그대로 저장한다.
- `event_id = sha256(source|native key...)` 로 **기기와 무관**하므로 같은 호출은 어느 기기·설치에서 올려도 한 번만 저장된다. 이미 있는 `event_id` 는 무시(`duplicates`)하되, 들어온 사본의 `output` 이 저장된 값보다 **크면** 토큰 수·`ts`·`reasoning`·`confidence`·`tool_version` 을 그 사본으로 갱신하고 `updated` 로 센다(기기·소스·모델·세션·`received_at` 은 유지). 스트리밍 응답의 첫 줄이 먼저 전송되고 완성된 줄이 뒤따라오는 경우를 위한 것이다.
- 운영자가 재수집을 요청한 기기(`DELETE /devices/{id}/events`, `POST /devices/{id}/resync`)에는 다음 응답 한 번에만 `"resync":true` 가 붙는다. 에이전트는 이를 받으면 커서와 오프라인 큐를 버리고 처음부터 다시 수집한다.

## 조회 (관리자 토큰)

집계 행(`summary` 의 `by_*`, `breakdown.rows`)은 한 가지 모양을 공유한다.
```json
{"key":"...","name":"(기기일 때)","source":"(단일 소스일 때)","device":"(기기 필터/그룹일 때)",
 "input":0,"output":0,"cache_read":0,"cache_write":0,"reasoning":0,
 "cost_usd":0.0,"cost_known_ratio":1.0,"estimated":false,"events":0,
 "first_seen":"...","last_seen":"..."}
```
- `cache_write` 는 5m + 1h + 일반 캐시 쓰기의 합계.
- 비용은 (모델 × UTC 날짜) 단위로 합산한 뒤 단가를 한 번 적용한다. 그룹 안에 단가를 모르는 토큰이 있으면 그 부분은 비용에서 빠지고 `cost_known_ratio`(단가가 있는 토큰의 비율)에 반영된다.
- `estimated` 는 pricing 의 `alias_of`/`estimated` 로 계산된 모델.

### GET /summary?from&to&tz
```json
{"from":"...","to":"...",
 "totals":{"input":0,"output":0,"cache_read":0,"cache_write":0,"reasoning":0,"cost_usd":0.0,"cost_known_ratio":0.97,"events":0},
 "by_source":[{"key":"claude_code", ...집계 행}],
 "by_device":[{"key":"<device_id>","name":"macbook", ...}],
 "by_model":[{"key":"claude-opus-5","source":"claude_code","estimated":false, ...}]}
```

### GET /timeseries?from&to&tz&bucket=day|hour&group_by=source|device|model|project|session
```json
{"bucket":"day","group_by":"source","tz":"Asia/Seoul",
 "series":[{"key":"claude_code","points":[{"t":"2026-09-01","input":0,"output":0,"cache_read":0,"cache_write":0,"reasoning":0,"cost_usd":0.0,"events":0}]}]}
```
- 기본값 `bucket=day`, `group_by=source`. `group_by=device` 시리즈에는 `name` 이 붙는다.
- `bucket=hour` 의 `t` 는 로컬 오프셋을 포함한 RFC3339. 데이터가 없는 버킷은 생략한다.

### GET /breakdown?from&to&group_by=model|project|device|session|source&source=&device=&limit=50
`{"group_by":"model","rows":[집계 행...]}`. 기본 `group_by=model`. 정렬은 비용 내림차순(동률이면 토큰 수).

### GET /throughput?from&to&tz&group_by=source|device|model|project|session&source=&device=
활동 분 기준 처리량. 이벤트의 완료 시각이 속한 분(minute)에 그 이벤트의 토큰을 귀속시키고, 이벤트가 하나라도 있는 분만 "활동 분"으로 센다. `per_minute` 는 활동 분 평균, `peak_*` 는 가장 많았던 1분(분 시작 시각은 `tz` 기준 RFC3339). `prompt` = input + cache_read + cache_write. `total` 은 그룹을 합친 값(활동 분은 합집합). 정렬은 `per_minute.total` 내림차순.
```json
{"group_by":"source","tz":"Asia/Seoul",
 "total":{"key":"","active_minutes":0,"prompt":0,"input":0,"cache_read":0,"cache_write":0,"output":0,"events":0,
          "per_minute":{"prompt":0.0,"output":0.0,"total":0.0},
          "peak_prompt":{"t":"2026-09-04T11:15:00+09:00","tokens":0},"peak_output":{"t":"...","tokens":0}},
 "groups":[{"key":"claude_code","name":"(기기일 때)", ...같은 모양}]}
```
빈 범위에서는 `peak_*` 가 생략된다. 완료 시각만 알기 때문에 응답 생성 속도(초당 토큰)가 아니라 "그 분에 완료된 토큰량" 의 근사치다.

### GET /events?from&to&source=&device=&model=&limit=200&cursor=
원본 이벤트를 최신순으로 페이지네이션한다. `{"events":[UsageEvent + "received_at","cost_usd","estimated"],"next_cursor":"..."}`
- `cost_usd` 는 단가 미상이면 `null`. `limit` 기본 200 / 최대 1000.
- `next_cursor` 는 `(ts, event_id)` 키셋 커서(불투명 base64url).

## 기기 관리 (관리자 토큰)

### GET /devices
`{"devices":[{"id","name","hostname","os","arch","agent_version","last_seen","last_ingest","events_total","token_created_at","revoked_at"}]}`. `revoked_at` 은 폐기된 기기에만 있다.

### POST /devices — 기기 등록 + 토큰 발급
`{"name":"macbook"}` → `201 {"id":"<uuid>","name":"macbook","token":"aum_...","token_created_at":"..."}`. 평문 토큰은 이 응답에만 나오고 서버에는 sha256 만 저장된다.

### POST /devices/{id}/rotate — 토큰 재발급
`201`, `POST /devices` 와 같은 모양. 기기 ID·이벤트는 그대로 두고 토큰만 바꾼다. 이전 토큰은 즉시 무효가 되고, 폐기된 기기는 rotate 로 되살아난다. 없으면 `404`.

### PATCH /devices/{id} — 이름 변경
`{"name":"거실 PC"}` → 기기 행. 이름은 운영자 소유이며, 에이전트가 보고하는 호스트명은 `hostname` 에 따로 보관된다(등록 시 이름이 비어 있을 때만 호스트명이 이름으로 쓰임).

### DELETE /devices/{id} — 토큰 폐기
`200 {"id":"...","revoked":true}`, 이벤트는 유지. `?purge=true` 면 그 기기의 이벤트와 기기 행까지 한 트랜잭션으로 지운다 → `200 {"id","deleted_events":n,"purged":true}`. `purge` 는 `true`/`1`/`yes`/`on`(대소문자 무시)만 참으로 본다. 없으면 `404`.

### DELETE /devices/{id}/events — 이벤트만 삭제
`200 {"id","deleted_events":n,"resync_requested":true}`. 기기·토큰은 유지하고 재수집 플래그를 세운다.

### POST /devices/{id}/resync — 삭제 없이 재수집 요청
`200 {"id","resync_requested":true}`. 에이전트가 처음부터 다시 보내면 같은 `event_id` 는 중복으로 버려지고, 더 완성된 사본만 갱신된다. 없으면 `404`.

### 운영 절차

| 하고 싶은 일 | 하는 법 |
|---|---|
| 기기 재등록 (토큰 분실/유출, 재설치) | `POST /devices/{id}/rotate` → 새 토큰으로 `agent install`. **새 기기를 만들지 말 것**: 한 대가 두 행으로 쪼개진다 |
| 기기 데이터 다시 맞추기 (수집 오류·중복 의심) | `DELETE /devices/{id}/events` 후 에이전트 주기(기본 60s, 유휴 기기는 하트비트 주기 5분)만큼 기다리면 전량 재전송된다. 서버에 못 붙는 기기는 그 기기에서 `ai-usage-agent rescan` |
| 기기 완전 삭제 | `DELETE /devices/{id}?purge=true`. 토큰만 무효화하고 이력은 남기려면 `?purge` 없이 |

재전송이 안전한 이유: `event_id` 가 기기와 무관해서 서버가 이미 가진 건 duplicate 로 흘린다.

## 단가 (관리자 토큰)

### GET /pricing
`{"rates":[{"match","provider","input","output","cache_read","cache_write","cache_write_5m","cache_write_1h","alias_of","estimated","effective_from","effective_to","note"}]}`. 같은 모델이 기간별로 여러 행일 수 있으며, 이벤트 날짜가 `[effective_from, effective_to)` 에 드는 첫 행이 적용된다.

### POST /pricing/reload
파일을 다시 읽는다 → `{"ok":true,"rates":n}`. 파싱에 실패하면 `400` 이고 기존 표가 유지된다.

## 헬스 체크 (토큰 불필요)

- `GET /health`, `GET /api/v1/health` → `{"ok":true,"version":"...","events":n,"devices":n,"checks":{"database":"ok","pricing":"ok","frontend":"ok"}}`. 하나라도 실패하면 `503`.
- `GET /health/live` → 프로세스 생존만 (liveness probe).
- `GET /health/ready` → DB 조회·단가표 로드·SPA 번들 포함·종료 중 아님을 모두 만족할 때 `200`, 아니면 `503` 과 `checks` (readiness/startup probe). SIGTERM 을 받으면 `SHUTDOWN_DELAY`(기본 8s) 동안 `503` 을 먼저 돌려 트래픽을 뺀 뒤 리스너를 닫는다.

## 다운로드 (토큰 불필요)

- `GET /downloads/` → `manifest.json` 기반 목록 (JSON; `Accept: text/html` 이면 간단한 HTML 표). `manifest.json` 이 없으면 `{"version":"","built_at":"","files":[]}`.
- `GET /downloads/manifest.json` → `{"version":"...","built_at":"...","files":[{"name":"ai-usage-agent_linux_amd64","os":"linux","arch":"amd64","size":0,"sha256":"..."}]}`
- `GET /downloads/<name>` → 바이너리 (`Content-Disposition: attachment`)
- `GET /downloads/install.sh` → Linux/macOS 설치 스크립트. 디렉터리에 `install.sh` 가 있으면 그 파일을, 없으면 서버가 내장 템플릿으로 생성한다.

## SPA 라우팅

`/api/*`, `/downloads/*`, `/health*` 외의 모든 GET 은 `index.html` 을 반환한다(클라이언트 라우팅).
