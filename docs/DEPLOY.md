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

## 볼륨

`DB_PATH` 가 있는 디렉터리 하나만 PVC 로 붙이면 된다. WAL 모드라 `usage.db`, `usage.db-wal`, `usage.db-shm` 세 파일이 같은 디렉터리에 생기므로 파일이 아니라 **디렉터리**를 마운트한다.

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
