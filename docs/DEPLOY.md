# 서버 배포

`cmd/server` 는 cgo 없는 단일 정적 바이너리다. `ko` 로 distroless/static 이미지에 올린다. 데이터는 기본으로 SQLite 파일 하나에 저장하며(`modernc.org/sqlite`), 설정 하나로 PostgreSQL 을 쓸 수도 있다([PostgreSQL](#postgresql) 참고).

## 설정

| 환경변수 | 플래그 | 기본값 | 설명 |
|---|---|---|---|
| `ADDR` | `--addr` | `:8080` | 리스닝 주소 |
| `DB_PATH` | `--db` | `data/usage.db` | SQLite 파일 경로(부모 디렉터리는 자동 생성), 또는 `postgres://` URL |
| `DATABASE_URL` | (환경변수 전용) | 없음 | `postgres://` URL. `--db`·`DB_PATH` 가 없을 때만 쓴다 |
| `DB_MAX_CONNS` | (환경변수 전용) | `10` | PostgreSQL 연결 풀 크기(서버 하나당). SQLite 에는 쓰지 않는다 |
| `PRICING_PATH` | `--pricing` | `pricing.yaml` | 단가 표. 파싱 실패하면 기동하지 않는다 |
| `DOWNLOADS_DIR` | `--downloads` | `$KO_DATA_PATH/downloads` (없으면 `cmd/server/kodata/downloads`) | `/downloads/` 로 서빙할 에이전트 바이너리 + `manifest.json` |
| `PUBLIC_URL` | `--public-url` | (없음: 요청의 Host·X-Forwarded-* 헤더로 추정) | 외부에서 접속하는 서버 주소(예: `https://usage.example.com`). 다운로드 페이지와 서버가 생성하는 `install.sh` 의 기본 주소로 씁니다. 헤더는 클라이언트가 바꿀 수 있으므로 외부에 공개하는 서버에서는 설정을 권장합니다. 로컬에서만 쓸 때는 비워 두면 됩니다 |
| `ADMIN_TOKEN` | (환경변수 전용) | 없음 | 관리/조회 API 토큰. **없으면 관리 API 는 전부 `503`** |

플래그를 주지 않으면 환경변수가, 둘 다 없으면 기본값이 쓰인다. `--version` 은 버전만 출력한다(`version` 은 ldflags 로 주입).
기동 로그에 설정이 한 줄 찍히지만 토큰 값은 절대 찍지 않는다(`admin_token_set=true/false` 만). DB URL 은 비밀번호를 `xxxxx` 로 가려서 찍고, 어느 설정에서 왔는지(`db_source`)도 함께 남긴다.

## GHCR 이미지로 실행

`main` 브랜치의 이미지는 `ghcr.io/lazyig/ai-usage-monitor:latest` 로 배포한다. 짧은 커밋 SHA(8자리)나 `v*` 태그로도 버전을 지정할 수 있다. 이미지는 `linux/amd64`, `linux/arm64` 를 지원하며 대시보드, 에이전트 6종, 설치/제거 스크립트, 단가표를 포함한다.

```sh
export ADMIN_TOKEN="$(openssl rand -hex 32)"
# 이 값을 안전한 곳에 보관한다. 대시보드 로그인과 관리 API 에 사용한다.
docker volume create ai-usage-data
# 서버는 uid 65532 로 실행된다. 새 볼륨은 root 소유이므로 한 번 소유자를 바꾼다.
docker run --rm -v ai-usage-data:/data busybox chown -R 65532:65532 /data
docker run -d --name ai-usage-monitor --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v ai-usage-data:/data \
  -e ADMIN_TOKEN \
  -e DB_PATH=/data/usage.db \
  -e PRICING_PATH=/var/run/ko/pricing.yaml \
  ghcr.io/lazyig/ai-usage-monitor:latest
```

`ko` 는 이미지의 데이터 경로를 `KO_DATA_PATH=/var/run/ko` 로 설정한다. 서버의 다운로드 경로는 이를 따라가지만, 단가표 기본값은 상대 경로 `pricing.yaml` 이므로 위처럼 `PRICING_PATH` 를 지정해야 한다. `/data` 볼륨에는 SQLite 데이터만 보존하며 `/var/run/ko` 는 마운트로 덮지 않는다. 외부에 공개할 때는 HTTPS 리버스 프록시를 앞에 둔다.

### 비 root 실행 (uid 65532)

이미지는 `distroless/static:nonroot` 기반이라 root 가 아닌 uid/gid `65532` 로 실행된다. 서버가 쓰는 곳은 `DB_PATH` 의 디렉터리뿐이므로 **그 볼륨이 uid 65532 에게 쓰기 가능해야** 한다. 임시 파일을 만들지 않으므로(`temp_store(MEMORY)`) `--read-only` 루트 파일시스템으로도 실행할 수 있다.

- 이미지에는 `/data` 가 없으므로 Docker 가 새로 만드는 이름 있는 볼륨은 root 소유(0755)다. 위 예시처럼 처음 한 번 `chown` 해야 한다.
- 이전(root 실행) 이미지로 만든 **기존** 볼륨의 디렉터리와 `usage.db`(·`-wal`·`-shm`)도 root 소유라 새 이미지가 열지 못한다. 업그레이드 전에 한 번만 소유자를 바꾼다(파일 내용과 모드는 그대로다).

  ```sh
  docker stop ai-usage-monitor
  docker run --rm -v ai-usage-data:/data busybox chown -R 65532:65532 /data
  ```

- 호스트 디렉터리를 바인드 마운트(`-v /srv/ai-usage:/data`)한다면 호스트에서 `sudo chown -R 65532:65532 /srv/ai-usage` 를 실행한다. 다른 uid 로 돌리려면 `--user <uid>:<gid>` 를 주고 볼륨을 그 uid 소유로 맞춘다. 소유자를 바꿀 수 없을 때 임시로 `--user 0:0` 을 줄 수 있지만 권장하지 않는다.

권한 문제로 DB 를 열지 못하면 서버는 `open store` 로그에 실행 중인 uid 와 `chown` 안내를 함께 남기고 종료한다.

GitHub Actions 는 `GITHUB_TOKEN` 으로 이미지를 게시한다. GHCR 패키지를 처음 만들면 패키지 설정에서 공개 여부를 확인하고, 익명으로 내려받으려면 visibility 를 Public 으로 설정한다.

## 볼륨 (SQLite)

PostgreSQL 을 쓰면 볼륨도, 아래의 `chown` 도 필요 없다. SQLite 는 `DB_PATH` 가 있는 디렉터리 하나만 PVC 로 붙이면 된다. WAL 모드라 `usage.db`, `usage.db-wal`, `usage.db-shm` 세 파일이 같은 디렉터리에 생기므로 파일이 아니라 **디렉터리**를 마운트한다.

```yaml
securityContext:                          # Pod
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532                          # PVC 를 uid 65532 가 쓸 수 있게 함
  seccompProfile: { type: RuntimeDefault }
containers:
  - name: server
    image: ghcr.io/lazyig/ai-usage-monitor:latest
    securityContext:                      # Container
      readOnlyRootFilesystem: true
      allowPrivilegeEscalation: false
      capabilities: { drop: [ALL] }
```

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

`fsGroup: 65532` 이면 kubelet 이 마운트할 때 PVC 의 그룹을 65532 로 바꾸고 그룹 쓰기 권한을 주므로, 대부분의 스토리지 클래스에서는 이전 root 이미지가 만든 기존 PVC 도 그대로 열린다. 다만 SQLite 가 새로 만드는 파일은 0600(소유자 전용)이므로 소유자도 맞춰 두는 편이 안전하다. `fsGroup` 을 지원하지 않는 볼륨(일부 NFS·hostPath 등)이나 `permission denied` 로 기동하지 않을 때는 한 번만 소유자를 바꾼다.

```sh
kubectl scale deploy/ai-usage-monitor --replicas=0
kubectl run fix-perms --rm -it --restart=Never --image=busybox --overrides='
{"spec":{"containers":[{"name":"fix-perms","image":"busybox","command":["chown","-R","65532:65532","/data"],
 "volumeMounts":[{"name":"data","mountPath":"/data"}]}],
 "volumes":[{"name":"data","persistentVolumeClaim":{"claimName":"ai-usage-data"}}]}}'
kubectl scale deploy/ai-usage-monitor --replicas=1
```

(`ai-usage-monitor`, `ai-usage-data` 는 실제 Deployment·PVC 이름으로 바꾼다.)

단가를 고쳤으면 재시작 없이 반영할 수 있다.

```sh
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" https://usage.example.com/api/v1/pricing/reload
```

## PostgreSQL

`DB_PATH`(또는 `--db`)에 `postgres://` URL 을 주거나, 둘 다 없을 때 `DATABASE_URL` 을 주면 PostgreSQL 을 쓴다. 첫 기동 때 스키마(`devices`, `usage_events`, `schema_migrations`)를 만들고, 이후 버전의 마이그레이션도 기동할 때 적용한다. API 와 대시보드는 SQLite 와 같은 결과를 낸다.

```
postgres://ai_usage@db.example.com:5432/ai_usage?sslmode=verify-full
```

- **비밀번호**는 URL 의 사용자 부분(`ai_usage:<비밀번호>@`, `@`·`/`·`?`·`#` 는 퍼센트 인코딩), `PGPASSWORD`, 또는 `PGPASSFILE` 로 준다. `?password=` 매개변수는 거부한다. 명령줄 플래그는 `ps` 에 보이므로 환경변수(k8s 는 Secret)를 쓴다.
- **암호화**: `sslmode` 를 주지 않으면 pgx 기본값 `prefer` 라 TLS 가 안 되면 평문으로 붙는다. 같은 기기(localhost·유닉스 소켓)가 아닌 호스트에 `require`/`verify-ca`/`verify-full` 없이 붙으면 기동할 때 경고한다. `sslmode=disable` 을 명시하면 일부러 고른 것으로 보고 경고하지 않는다(사설망 안의 compose 등).
- **권한**: 마이그레이션을 위해 대상 스키마에 `CREATE` 권한이 필요하다. 다른 스키마를 쓰려면 `?search_path=ai_usage` 를 붙인다.
- **연결 수**: 서버 하나가 최대 `DB_MAX_CONNS`(기본 10) + 2(헬스 체크용) 개를 연다. `서버 수 × (DB_MAX_CONNS + 2)` 가 `max_connections` 보다 넉넉히 작게 잡는다.
- **긴 쿼리 제한**: 넓은 기간의 집계가 연결을 오래 잡지 않도록 `?options=-c%20statement_timeout%3D60s` 를 권장한다(마이그레이션은 이 제한을 받지 않는다).
- **PgBouncer**: session pooling 이면 그대로 된다. transaction pooling 이면 `?default_query_exec_mode=exec` 를 붙인다(이름 있는 prepared statement 를 쓰지 않는다). PgBouncer 는 `options`·`search_path` 같은 시작 매개변수를 `ignore_startup_parameters` 에 넣지 않으면 거부하므로, 그때는 `statement_timeout` 을 DB 사용자에 걸어 둔다(`ALTER ROLE ai_usage SET statement_timeout = '60s'`).
- **기동 순서**: DB 가 아직 뜨지 않았으면 최대 60초 동안 다시 시도한다(연결 시도 하나는 기본 10초 안에 끝나고, SIGTERM 이면 바로 멈춘다). 비밀번호가 틀렸거나 DB 가 없으면 기다리지 않고 바로 종료한다. 그동안은 HTTP 를 열지 않으므로, k8s 에서는 아래처럼 `startupProbe` 를 둬 liveness 가 그 사이에 Pod 를 죽이지 않게 한다.
- **안전장치**: `DATABASE_URL` 만 있고 기본 SQLite 파일(`data/usage.db`)이 이미 있으면 기동하지 않는다. 빈 PostgreSQL 로 조용히 바뀌어 데이터가 사라진 것처럼 보이는 일을 막기 위해서다. SQLite 를 계속 쓰려면 `DB_PATH` 를 그 파일로, 일부러 바꾸려면 `DB_PATH` 를 PostgreSQL URL 로 준다. `DB_PATH` 와 `DATABASE_URL` 이 둘 다 있으면 `DB_PATH` 를 쓰고 경고만 남긴다.

docker compose 예시:

```yaml
services:
  db:
    image: postgres:16
    environment:
      POSTGRES_USER: ai_usage
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_DB: ai_usage
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ai_usage"]
      interval: 2s
      retries: 30
  server:
    image: ghcr.io/lazyig/ai-usage-monitor:latest
    depends_on:
      db: { condition: service_healthy }
    ports: ["127.0.0.1:8080:8080"]
    environment:
      ADMIN_TOKEN: ${ADMIN_TOKEN}
      PRICING_PATH: /var/run/ko/pricing.yaml
      # 같은 compose 네트워크 안이라 TLS 없이 붙는다.
      DATABASE_URL: postgres://ai_usage@db:5432/ai_usage?sslmode=disable
      PGPASSWORD: ${DB_PASSWORD}
volumes:
  pgdata:
```

Kubernetes 에서는 URL 과 비밀번호를 Secret 으로 넣는다. 위 SQLite 매니페스트의 `DB_PATH` env 는 **지운다**(남아 있으면 `DB_PATH` 가 이겨 SQLite 를 계속 쓰고 경고만 남는다). PVC 와 `fsGroup` 도 빼도 된다.

```yaml
env:
  - name: DATABASE_URL
    valueFrom: { secretKeyRef: { name: ai-usage-monitor, key: database-url } }
  - name: PGPASSWORD
    valueFrom: { secretKeyRef: { name: ai-usage-monitor, key: database-password } }
startupProbe:                      # DB 를 기다리는 최대 60초 + 여유
  httpGet: { path: /health/ready, port: 8080 }
  periodSeconds: 5
  failureThreshold: 20
livenessProbe:
  httpGet: { path: /health/live, port: 8080 }
readinessProbe:
  httpGet: { path: /health/ready, port: 8080 }
```

### SQLite 에서 옮기기 (`copy-db`)

`ai-usage-server copy-db` 는 기기(토큰 해시 포함)와 이벤트를 다른 DB 로 그대로 복사한다. 기기 토큰이 그대로 옮겨지므로 에이전트는 다시 설치하지 않아도 된다. 반대 방향(PostgreSQL → SQLite)도 된다.

- 대상은 비어 있어야 한다(없으면 스키마를 만든다). 원본은 읽기만 하고 마이그레이션하지 않으므로, 원본이 이 버전의 스키마가 아니면 먼저 이 버전 서버를 원본으로 한 번 띄운다.
- 원본은 한 스냅샷으로 읽고 대상은 한 트랜잭션으로 쓴다. 끝나면 양쪽 행 수를 비교해 출력한다. 실패하면 대상에는 아무것도 남지 않는다.
- 복사하는 동안 들어온 이벤트는 옮겨지지 않으므로 **서버를 멈추고** 실행한다. 에이전트는 그동안 보내지 못한 이벤트를 큐에 두었다가 새 서버로 다시 보낸다.
- `-to` 를 주지 않으면 서버 자신의 설정(`DB_PATH`, 없으면 `DATABASE_URL`)을 대상으로 쓴다. 비밀번호가 `ps` 에 보이지 않도록 URL 은 `-from-env`/`-to-env` 와 `PGPASSWORD` 로 넘긴다.
- 예전 SQLite 데이터의 자유 텍스트에 NUL·깨진 UTF-8 이 있으면 U+FFFD 로 바꿔 넣고 그 개수를 알려 준다. id 에 그런 문자가 있으면 복사하지 않고 해당 id 를 보여 준다.

Docker:

```sh
docker stop ai-usage-monitor
docker run --rm -v ai-usage-data:/data \
  -e TARGET='postgres://ai_usage@db.example.com/ai_usage?sslmode=verify-full' -e PGPASSWORD \
  ghcr.io/lazyig/ai-usage-monitor:latest copy-db -from /data/usage.db -to-env TARGET
# copied 3 devices and 182311 events; counts verified
```

그다음 서버를 `DB_PATH` 없이 `DATABASE_URL` 로(또는 `DB_PATH` 에 그 URL 을 주고) 다시 띄운다. `DB_PATH=/data/usage.db` 가 남아 있으면 SQLite 를 계속 쓰므로 꼭 바꾼다. 옛 볼륨은 서버에서 떼어 내고, 확인이 끝날 때까지 백업으로 남겨 둔다. 원본에 기기도 이벤트도 없으면 `-from` 을 잘못 준 것으로 보고 거부한다(일부러면 `-allow-empty`).

Kubernetes: Deployment 를 0으로 줄이고, PVC 를 **읽기·쓰기로** 붙인 Job 으로 실행한다(SQLite 가 읽기 전용 열기에도 `-shm` 파일을 쓴다).

```yaml
apiVersion: batch/v1
kind: Job
metadata: { name: ai-usage-copy-db }
spec:
  backoffLimit: 0
  template:
    spec:
      restartPolicy: Never
      securityContext: { runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532 }
      containers:
        - name: copy
          image: ghcr.io/lazyig/ai-usage-monitor:latest
          args: [copy-db, -from, /data/usage.db, -to-env, DATABASE_URL]
          env:
            - name: DATABASE_URL
              valueFrom: { secretKeyRef: { name: ai-usage-monitor, key: database-url } }
            - name: PGPASSWORD
              valueFrom: { secretKeyRef: { name: ai-usage-monitor, key: database-password } }
          volumeMounts: [{ name: data, mountPath: /data }]
      volumes:
        - { name: data, persistentVolumeClaim: { claimName: ai-usage-data } }
```

```sh
kubectl scale deploy/ai-usage-monitor --replicas=0
kubectl apply -f copy-db-job.yaml
kubectl wait --for=condition=complete --timeout=30m job/ai-usage-copy-db   # Job 이 실패해도 시간 제한까지 기다리므로 kubectl get job 으로 상태를 함께 본다
kubectl logs job/ai-usage-copy-db          # copied N devices and M events; counts verified
# Deployment 의 DB_PATH 를 지우고 DATABASE_URL·PGPASSWORD·startupProbe 를 넣은 뒤
kubectl scale deploy/ai-usage-monitor --replicas=1
```

### 서버 여러 대

PostgreSQL 이면 서버를 여러 대 띄울 수 있다(SQLite 는 한 대만). 동시에 기동해도 마이그레이션은 advisory lock 으로 한 번만 적용된다. 주의할 점:

- `POST /api/v1/pricing/reload` 는 요청을 받은 서버만 다시 읽는다. 단가표를 바꾸면 Deployment 를 다시 시작(`kubectl rollout restart`)한다.
- 기기 토큰은 서버마다 60초 캐시한다. 폐기·재발급은 처리한 서버에서는 바로, 다른 서버에서는 최대 60초 뒤에 적용된다.
- 롤링 업데이트 중에는 옛 버전과 새 버전이 같은 스키마를 쓴다. 새 마이그레이션은 이전 버전이 계속 동작하도록(열 추가 위주) 작성한다.

## 백업

- **SQLite**: 서버를 멈추고 `usage.db`(·`-wal`·`-shm`)를 복사하거나, 실행 중이면 `sqlite3 /data/usage.db ".backup /backup/usage.db"` 를 쓴다(파일만 복사하면 WAL 에 남은 내용이 빠질 수 있다).
- **PostgreSQL**: `pg_dump -Fc` 로 백업하고 `pg_restore` 로 복원한다.

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
# PostgreSQL 로: DB_PATH='postgres://postgres@localhost/ai_usage?sslmode=disable' ADMIN_TOKEN=dev go run ./cmd/server
curl localhost:8080/health
curl -H 'Authorization: Bearer dev' 'localhost:8080/api/v1/summary?from=2026-09-01&to=2026-09-03'
```

`web/dist` 가 없으면 `/` 는 "SPA not built" 안내를 준다. 대시보드까지 보려면 `cd web && npm ci && npm run build` 후 다시 실행한다(`go:embed` 라 재빌드 필요).
