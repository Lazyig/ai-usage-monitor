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
→ `200 {"accepted":12,"duplicates":3}`. 배치 최대 5,000건. `device.id` 는 토큰에 묶인 기기와 일치해야 함(불일치 403).
`event_id` 중복은 무시(UPSERT 아님, 최초 값 유지).

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

### GET /events?from&to&source=&device=&model=&limit=200&cursor=
원본 이벤트 페이지네이션. `{"events":[UsageEvent + "cost_usd"],"next_cursor":"..."}`

### GET /devices
`{"devices":[{"id","name","os","arch","agent_version","last_seen","last_ingest","events_total","token_created_at"}]}`

### POST /devices  → 기기 등록 + 토큰 발급 (토큰은 이 응답에만 평문 노출)
`{"name":"macbook"}` → `{"id":"<uuid>","name":"macbook","token":"aum_..."}`

### DELETE /devices/{id}  → 토큰 폐기 (이벤트는 유지)

### GET /pricing  → 현재 로드된 pricing.yaml 항목 목록 `{"rates":[{"match","provider","input","output","cache_read","cache_write","cache_write_5m","cache_write_1h","alias_of","estimated"}]}`
### POST /pricing/reload  → 파일 다시 읽기

### GET /health  (토큰 불필요) → `{"ok":true,"version":"...","events":n}`

## 다운로드 (토큰 불필요)

- `GET /downloads/` → `manifest.json` 기반 목록 (JSON; `Accept: text/html` 이면 간단한 HTML 표)
- `GET /downloads/manifest.json` → `{"version":"<git tag or short sha>","built_at":"...","files":[{"name":"ai-usage-agent_linux_amd64","os":"linux","arch":"amd64","size":0,"sha256":"..."}]}`
- `GET /downloads/<name>` → 바이너리 (`Content-Disposition: attachment`)
- `GET /downloads/install.sh` → linux/mac 용 설치 스크립트 (서버 URL 과 기기 토큰을 인자로 받아 `agent install` 까지 수행)

## SPA 라우팅

`/api/*`, `/downloads/*`, `/health` 외의 모든 GET 은 `index.html` 을 반환 (클라이언트 라우팅).
