# HTTP API v1

모든 경로는 `/api/v1` 아래. 인증은 `Authorization: Bearer <token>`.
- 기기 토큰: `ingest` 만 허용
- 관리자 토큰(`ADMIN_TOKEN` 환경변수): 나머지 전부
- 실패: `401 {"error":"unauthorized"}`. 그 외 오류는 `{"error":"<message>"}` 와 4xx/5xx.
- 시각은 RFC3339 UTC. 금액은 USD float. 토큰 수는 정수.
- 기간 파라미터: `from`, `to` (RFC3339 또는 `YYYY-MM-DD`, to 는 exclusive). 기본 최근 30일. `tz` (IANA, 기본 `Asia/Seoul`) 는 일 단위 버킷 경계에만 사용.

## 수집

### POST /ingest  (기기 토큰)
```json
{"device":{"id":"<uuid>","name":"macbook","os":"darwin","arch":"arm64","agent_version":"0.1.0"},
 "events":[{ ...UsageEvent JSON (internal/event) ... }]}
```
→ `200 {"accepted":12,"duplicates":3,"rejected":0}`. 배치 최대 5,000건.
`rejected` 는 `event.Valid()` 실패 또는 `device_id` 불일치로 버려진 이벤트 수(나머지는 그대로 저장). `device.id` 는 토큰에 묶인 기기와 일치해야 함(불일치 403).
`event_id` 중복은 무시(UPSERT 아님, 최초 값 유지). `event_id = sha256(source|native key...)` 로 **기기와 무관**하므로, 같은 호출은 어느 기기·설치에서 올려도 한 번만 저장된다.

`DELETE /devices/{id}/events` 로 재수집 플래그가 선 기기에는 응답에 `"resync":true` 가 추가된다(플래그는 이때 소비되어 딱 한 번만 나감. 빈 배치=하트비트도 포함). 에이전트는 이걸 받으면 커서와 오프라인 큐를 버리고 즉시 처음부터 다시 수집한다.

## 조회 (관리자 토큰)

### GET /summary?from&to&tz
```json
{"from":"...","to":"...",
 "totals":{"input":0,"output":0,"cache_read":0,"cache_write":0,"reasoning":0,"cost_usd":0.0,"cost_known_ratio":0.97,"events":0},
 "by_source":[{"key":"claude_code","input":0,"output":0,"cache_read":0,"cache_write":0,"cost_usd":0.0,"events":0}],
 "by_device":[{"key":"<device_id>","name":"macbook", ...same fields}],
 "by_model":[{"key":"claude-opus-5","source":"claude_code","estimated":false, ...}]}
```
`cost_known_ratio` = 단가가 있는 이벤트의 토큰 비율. `estimated` 는 pricing alias 로 계산된 모델.

### GET /timeseries?from&to&tz&bucket=day|hour&group_by=source|device|model|project
```json
{"bucket":"day","group_by":"source",
 "series":[{"key":"claude_code","points":[{"t":"2026-09-01","input":0,"output":0,"cache_read":0,"cache_write":0,"cost_usd":0.0}]}]}
```

### GET /breakdown?from&to&group_by=model|project|device|session&source=&device=&limit=50
```json
{"group_by":"model","rows":[{"key":"claude-opus-5","source":"claude_code","device":"...","input":0,"output":0,"cache_read":0,"cache_write":0,"reasoning":0,"cost_usd":0.0,"estimated":false,"events":0,"first_seen":"...","last_seen":"..."}]}
```

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
원본 이벤트 페이지네이션. `{"events":[UsageEvent + "cost_usd"],"next_cursor":"..."}`

### GET /devices
`{"devices":[{"id","name","os","arch","agent_version","last_seen","last_ingest","events_total","token_created_at"}]}`

### POST /devices  → 기기 등록 + 토큰 발급 (토큰은 이 응답에만 평문 노출)
`{"name":"macbook"}` → `{"id":"<uuid>","name":"macbook","token":"aum_..."}`

### POST /devices/{id}/rotate  → 토큰 재발급 (평문 토큰은 이 응답에만 노출)
`201 {"id","name","token":"aum_...","token_created_at"}`. 기기 ID·이벤트는 그대로 두고 토큰만 바꾼다. 이전 토큰은 즉시 무효(캐시도 바로 만료), 폐기된 기기는 rotate 로 되살아난다. 없으면 `404`.

### PATCH /devices/{id}  → 이름 변경 `{"name":"거실 PC"}` → 기기 행. 이름은 운영자 소유이며 에이전트가 보고하는 호스트명은 `hostname` 필드에 따로 보관된다(등록 시 이름이 비어 있을 때만 호스트명이 이름으로 채워짐).

### DELETE /devices/{id}  → 토큰 폐기 (이벤트는 유지)
`?purge=true` 를 붙이면 그 기기의 이벤트와 기기 행까지 한 트랜잭션으로 완전 삭제 → `200 {"id","deleted_events":n,"purged":true}`.

### DELETE /devices/{id}/events  → 이벤트만 삭제 (기기·토큰 유지)
`200 {"id","deleted_events":n,"resync_requested":true}`. 재수집 플래그가 서고, 그 기기의 다음 `POST /ingest` 응답이 `"resync":true` 를 딱 한 번 실어 보낸다.

### GET /pricing  → 현재 로드된 pricing.yaml 항목 목록 `{"rates":[{"match","provider","input","output","cache_read","cache_write","cache_write_5m","cache_write_1h","alias_of","estimated","effective_from","effective_to","note"}]}`. 같은 모델이 기간별로 여러 행일 수 있으며 이벤트 날짜가 `[effective_from, effective_to)` 에 드는 첫 행이 적용된다.
### POST /pricing/reload  → 파일 다시 읽기

### GET /health  (토큰 불필요) → `{"ok":true,"version":"...","events":n,"devices":n,"checks":{"database":"ok","pricing":"ok","frontend":"ok"}}` — 하나라도 실패하면 503
### GET /health/live  → 프로세스 생존만 (liveness probe)
### GET /health/ready → DB 조회·단가표 로드·SPA 번들 포함·종료 중 아님을 모두 만족할 때 200, 아니면 503 과 `checks` (readiness/startup probe). SIGTERM 을 받으면 `SHUTDOWN_DELAY`(기본 8s) 동안 503 을 먼저 돌려 트래픽을 뺀 뒤 리스너를 닫는다.

## 다운로드 (토큰 불필요)

- `GET /downloads/` → `manifest.json` 기반 목록 (JSON; `Accept: text/html` 이면 간단한 HTML 표)
- `GET /downloads/manifest.json` → `{"version":"<git tag or short sha>","built_at":"...","files":[{"name":"ai-usage-agent_linux_amd64","os":"linux","arch":"amd64","size":0,"sha256":"..."}]}`
- `GET /downloads/<name>` → 바이너리 (`Content-Disposition: attachment`)
- `GET /downloads/install.sh` → linux/mac 용 설치 스크립트 (서버 URL 과 기기 토큰을 인자로 받아 `agent install` 까지 수행)

## SPA 라우팅

`/api/*`, `/downloads/*`, `/health` 외의 모든 GET 은 `index.html` 을 반환 (클라이언트 라우팅).

---

## 구현 메모 (서버 v1)

`docs/API.md` 의 계약을 그대로 구현하면서 아래 필드/동작을 **추가**했다. 기존 필드는 그대로다.

**공통**
- `from`/`to` 를 `YYYY-MM-DD` 로 주면 `tz` 기준 자정으로 해석한다(`to` 는 exclusive). RFC3339 는 그대로 절대 시각.
- 기본 구간: `to` = 지금, `from` = `to` 가 속한 `tz` 날짜의 자정에서 29일 전 → 로컬 기준 최근 30일.
- 모든 `/api/*` 응답에 `Cache-Control: no-store`.
- 요청 본문은 8 MiB 까지(초과 시 `413`). `ADMIN_TOKEN` 미설정이면 관리 API 는 `503 {"error":"admin token not configured"}`.
- `POST /ingest` 의 `device.id` 불일치는 `403`, 5,000건 초과는 `400`.

**집계 행 (`by_source`/`by_device`/`by_model`/`breakdown.rows`) 은 한 가지 모양을 공유한다**
```json
{"key":"...","name":"(기기일 때)","source":"(단일 소스일 때)","device":"(기기 필터/그룹일 때)",
 "input":0,"output":0,"cache_read":0,"cache_write":0,"reasoning":0,
 "cost_usd":0.0,"cost_known_ratio":1.0,"estimated":false,"events":0,
 "first_seen":"...","last_seen":"..."}
```
`cache_write` 는 5m+1h+generic 합계. 비용은 (모델 × UTC 날짜) 단위로 합산 후 `pricing.Table.Cost` 로 한 번 계산한다(토큰 종류별 선형이라 이벤트별 합과 같고, `effective_from` 경계도 지켜진다). 그룹 안에 단가 없는 토큰이 있으면 그 그룹의 비용은 미상으로 처리되고 `cost_known_ratio` 에 반영된다.

**GET /timeseries**
- 응답에 `tz` 추가. `bucket` 기본 `day`, `group_by` 기본 `source`, `group_by=device` 인 시리즈에는 `name` 추가.
- 포인트에 `reasoning`, `events` 추가. `bucket=hour` 의 `t` 는 RFC3339(로컬 오프셋 포함).
- 데이터가 없는 버킷은 생략한다.

**GET /breakdown** — `group_by` 에 `source` 도 허용, 기본값 `model`. 정렬은 비용 내림차순(동률이면 토큰 수).

**GET /events** — 각 이벤트에 `received_at`, `cost_usd`(단가 미상이면 `null`), `estimated` 추가. `next_cursor` 는 `(ts, event_id)` 키셋 커서(불투명 base64url). 정렬은 최신순, `limit` 기본 200 / 최대 1000.

**devices**
- `GET /devices` 행에 `revoked_at`(폐기된 기기만) 추가.
- `POST /devices` → `201`, 응답에 `token_created_at` 추가.
- `DELETE /devices/{id}` → `200 {"id":"...","revoked":true}`, 없으면 `404`. `?purge=true` 면 `{"id","deleted_events":n,"purged":true}`.
- `POST /devices/{id}/rotate` → `201`, `POST /devices` 와 같은 모양(`id`,`name`,`token`,`token_created_at`).
- `DELETE /devices/{id}/events` → `200 {"id","deleted_events":n,"resync_requested":true}`.
- `purge=true` 는 `true`/`1`/`yes`/`on`(대소문자 무시)만 참으로 본다. 값이 없거나 다른 값이면 평범한 폐기.

**운영 절차**

| 하고 싶은 일 | 하는 법 |
|---|---|
| 기기 재등록 (토큰 분실/유출, 재설치) | `POST /devices/{id}/rotate` → 새 토큰으로 `agent install`. **새 기기를 만들지 말 것**: 한 대가 두 행으로 쪼개진다 |
| 기기 데이터 다시 맞추기 (수집 오류·중복 의심) | `DELETE /devices/{id}/events` 후 에이전트 주기(기본 60s, 유휴 기기는 하트비트 주기 5분)만큼 기다리면 알아서 전량 재전송. 서버에 못 붙는 기기는 그 기기에서 `ai-usage-agent rescan` |
| 기기 완전 삭제 | `DELETE /devices/{id}?purge=true` (이벤트+기기 행). 토큰만 죽이고 이력은 남기려면 `?purge` 없이 |

재전송이 안전한 이유: `event_id` 가 기기와 무관해서(위 ingest 항목) 서버가 이미 가진 건 duplicate 로 흘린다.

**pricing** — `GET /pricing` 항목에 `effective_from` 추가. `POST /pricing/reload` → `{"ok":true,"rates":n}`; 파싱 실패 시 `400` 이고 기존 표가 유지된다.

**health** — `{"ok":true,"version":"...","events":n,"devices":n}`. `/health` 와 `/api/v1/health` 둘 다 응답.

**downloads** — `manifest.json` 이 없으면 `GET /downloads/` 는 `{"version":"","built_at":"","files":[]}`. 디렉터리에 `install.sh` 가 있으면 그 파일을, 없으면 서버가 템플릿으로 생성해서 준다.
