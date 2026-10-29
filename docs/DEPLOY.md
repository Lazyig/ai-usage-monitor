# 서버 배포

`cmd/server` 는 cgo 없는 단일 정적 바이너리다(`modernc.org/sqlite`). `ko` 로 distroless/static 이미지에 올리고, SQLite 파일 하나만 보존하면 된다.

## 설정

| 환경변수 | 플래그 | 기본값 | 설명 |
|---|---|---|---|
| `ADDR` | `--addr` | `:8080` | 리스닝 주소 |
| `DB_PATH` | `--db` | `data/usage.db` | SQLite 파일 (부모 디렉터리는 자동 생성) |
| `PRICING_PATH` | `--pricing` | `pricing.yaml` | 단가 표. 파싱 실패하면 기동하지 않는다 |
| `DOWNLOADS_DIR` | `--downloads` | `$KO_DATA_PATH/downloads` (없으면 `cmd/server/kodata/downloads`) | `/downloads/` 로 서빙할 에이전트 바이너리 + `manifest.json` |
| `PUBLIC_URL` | `--public-url` | (없음: 요청의 Host·X-Forwarded-* 헤더로 추정) | 외부에서 접속하는 서버 주소(예: `https://usage.example.com`). 다운로드 페이지와 서버가 생성하는 `install.sh` 의 기본 주소로 씁니다. 헤더는 클라이언트가 바꿀 수 있으므로 외부에 공개하는 서버에서는 설정을 권장합니다. 로컬에서만 쓸 때는 비워 두면 됩니다 |
| `ADMIN_TOKEN` | (환경변수 전용) | 없음 | 관리/조회 API 토큰. **없으면 관리 API 는 전부 `503`** |

플래그를 주지 않으면 환경변수가, 둘 다 없으면 기본값이 쓰인다. `--version` 은 버전만 출력한다(`version` 은 ldflags 로 주입).
기동 로그에 설정이 한 줄 찍히지만 토큰 값은 절대 찍지 않는다(`admin_token_set=true/false` 만).

## GHCR 이미지로 실행

`main` 브랜치의 이미지는 `ghcr.io/lazyig/ai-usage-monitor:latest` 로 배포한다. 짧은 커밋 SHA(8자리)나 `v*` 태그로도 버전을 지정할 수 있다. 이미지는 `linux/amd64`, `linux/arm64` 를 지원하며 대시보드, 에이전트 6종, 설치/제거 스크립트, 단가표를 포함한다.

```sh
export ADMIN_TOKEN="$(openssl rand -hex 32)"
# 이 값을 안전한 곳에 보관한다. 대시보드 로그인과 관리 API 에 사용한다.
docker volume create ai-usage-data
docker run -d --name ai-usage-monitor --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v ai-usage-data:/data \
  -e ADMIN_TOKEN \
  -e DB_PATH=/data/usage.db \
  -e PRICING_PATH=/var/run/ko/pricing.yaml \
  ghcr.io/lazyig/ai-usage-monitor:latest
```

`ko` 는 이미지의 데이터 경로를 `KO_DATA_PATH=/var/run/ko` 로 설정한다. 서버의 다운로드 경로는 이를 따라가지만, 단가표 기본값은 상대 경로 `pricing.yaml` 이므로 위처럼 `PRICING_PATH` 를 지정해야 한다. `/data` 볼륨에는 SQLite 데이터만 보존하며 `/var/run/ko` 는 마운트로 덮지 않는다. 외부에 공개할 때는 HTTPS 리버스 프록시를 앞에 둔다.

GitHub Actions 는 `GITHUB_TOKEN` 으로 이미지를 게시한다. GHCR 패키지를 처음 만들면 패키지 설정에서 공개 여부를 확인하고, 익명으로 내려받으려면 visibility 를 Public 으로 설정한다.

## 볼륨

`DB_PATH` 가 있는 디렉터리 하나만 PVC 로 붙이면 된다. WAL 모드라 `usage.db`, `usage.db-wal`, `usage.db-shm` 세 파일이 같은 디렉터리에 생기므로 파일이 아니라 **디렉터리**를 마운트한다.

```yaml
env:
  - name: DB_PATH
    value: /data/usage.db
  - name: PRICING_PATH
    value: /config/pricing.yaml       # ConfigMap 마운트
  - name: ADMIN_TOKEN
    valueFrom: { secretKeyRef: { name: ai-usage-monitor, key: admin-token } }
volumeMounts:
  - { name: data, mountPath: /data }              # PVC
  - { name: pricing, mountPath: /config }         # ConfigMap
livenessProbe:
  httpGet: { path: /health/live, port: 8080 }
readinessProbe:
  httpGet: { path: /health/ready, port: 8080 }
```

단가를 고쳤으면 재시작 없이 반영할 수 있다.

```sh
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" https://usage.example.com/api/v1/pricing/reload
```

## 첫 기기 토큰 발급

토큰은 발급 응답에만 평문으로 나온다(저장은 sha256 만). 잃어버리면 폐기하고 다시 발급한다.

```sh
curl -X POST https://usage.example.com/api/v1/devices \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"macbook"}'
# {"id":"<uuid>","name":"macbook","token":"aum_...","token_created_at":"..."}
```

그 토큰으로 기기에 에이전트를 설치한다.

```sh
curl -fsSL https://usage.example.com/downloads/install.sh | sh -s -- \
  --server https://usage.example.com --token aum_...
```

폐기는 `DELETE /api/v1/devices/<id>` (이벤트는 남고 토큰만 무효화, 즉시 적용).

## 로컬 실행

```sh
ADMIN_TOKEN=dev go run ./cmd/server            # :8080, data/usage.db, pricing.yaml
curl localhost:8080/health
curl -H 'Authorization: Bearer dev' 'localhost:8080/api/v1/summary?from=2026-09-01&to=2026-09-03'
```

`web/dist` 가 없으면 `/` 는 "SPA not built" 안내를 준다. 대시보드까지 보려면 `cd web && npm ci && npm run build` 후 다시 실행한다(`go:embed` 라 재빌드 필요).
