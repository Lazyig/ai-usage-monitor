# 개발과 검증

Go 버전은 `go.mod`, 웹 의존성은 `web/package-lock.json`을 기준으로 맞춥니다. CI의 Node 버전은 22입니다. Go 테스트 중 Unix 설치 검증은 `sh`와 `curl`을 사용합니다.

## 코드 경계

- 수집기는 로그를 읽고 `UsageEvent`와 커서를 반환합니다. 전송·큐·서비스 관리는 수집기에 넣지 않습니다.
- 이벤트 ID는 재전송해도 같아야 합니다. 서버 저장은 멱등성을 유지하고, 전송 실패한 데이터는 안전하게 보관한 뒤에만 커서를 진행합니다.
- 일시 실패(408/429/5xx), 인증 실패, 영구적인 요청 오류를 구분합니다. HTTP 2xx만으로 잘못된 응답을 정상 수신 확인으로 취급하지 않습니다.
- SQLite SQL과 트랜잭션은 `internal/store`, HTTP 인증·입출력은 `internal/httpapi`에 둡니다. 동적 SQL에는 허용 목록에 있는 컬럼만 사용합니다.
- 웹 JSON 타입은 서버의 실제 응답과 맞춥니다. 요청 경합·페이지네이션 상태는 훅에서, 마크업은 페이지에서 처리합니다. 필터가 바뀌면 성공 응답뿐 아니라 오류와 완료 처리도 이전 요청인지 검사합니다.
- React 행 키는 모델 이름만으로 만들지 않습니다. 적용 기간 등 행의 실제 식별자를 포함합니다.
- Unix 설치 스크립트의 원본은 `scripts/install.sh` 하나입니다. 서버 fallback은 이를 embed하며 별도 설치 구현을 추가하지 않습니다. 검증 정보가 없는 파일을 설치하지 않습니다.

## 기본 검사

```sh
gofmt -w <변경한 Go 파일>
go vet ./...
go test -race ./...
npm ci --prefix web
npm --prefix web test
npm --prefix web run build
pwsh -NoProfile -File scripts/install_test.ps1
VERSION=review scripts/build-agents.sh /tmp/ai-usage-agents-review
```

`npm run build`는 앱뿐 아니라 브라우저 테스트 설정/코드도 타입 검사합니다. 생성된 `web/dist/index.html`과 `.gitkeep`의 작업 트리 변경은 소스 수정과 구분하고 커밋에 포함하지 않습니다. 테스트를 위해 실제 사용자 로그나 운영 토큰을 사용하지 않습니다.

기존 실제 홈 로그 통합 검사는 기본적으로 건너뜁니다. 본인의 실제 로그를 읽기 전용으로 검사하려는 경우에만 `AI_USAGE_INTEGRATION=1 go test ./internal/collector/... -run TestIntegrationRealHome`으로 명시적으로 실행합니다. 일반 검증과 CI는 이 플래그 없이 고정 픽스처를 검사합니다.

## 실제 브라우저 검증

브라우저 테스트는 loopback 서버만 허용합니다. 운영 서버를 포트 포워딩하지 말고 새 임시 DB를 사용합니다. 픽스처 도구도 비어 있는 DB만 채우며, 전달받은 DB가 동일한 테스트 기기만 담고 있는지 확인합니다.

```sh
npm --prefix web run build
go build -o /tmp/review-server ./cmd/server
mkdir -p /tmp/review-downloads
ADMIN_TOKEN=review-only-token SHUTDOWN_DELAY=0 /tmp/review-server \
  --addr 127.0.0.1:18081 --db /tmp/review.db --pricing pricing.yaml \
  --downloads /tmp/review-downloads
```

별도 터미널에서 다음을 실행합니다. `review-only-token`은 이 임시 서버 전용의 공개 테스트 값입니다.

```sh
python3 scripts/review-fixture.py --db /tmp/review.db --downloads /tmp/review-downloads \
  --api-snapshot /tmp/review-api-before.json
cd web
npx playwright install --with-deps chromium
npm run test:e2e
```

기본 테스트는 정상 화면과 로그인/로그아웃, 날짜 필터, 새로고침, 이벤트 페이지네이션, 기기 생성/이름 변경/재동기화/초기화/토큰 관리/삭제, 가격표 재로드를 검사합니다. 데스크톱·모바일 각각 라이트·다크 모드로 실행합니다. 기기 변경은 샘플 DB에만 수행하며 종료 시 정리합니다.

디자인 비교는 **변경 전 코드**를 위 서버로 실행한 상태에서 기준을 만든 뒤, 동일 DB·포트·브라우저·글꼴·환경 변수로 **변경 후 코드**를 실행해 비교합니다. 기존 기준을 새 코드에서 갱신하면 회귀를 검증할 수 없습니다.

```sh
# 변경 전 서버: 기준 캡처
REVIEW_SCREENSHOTS=1 npm run test:e2e -- 00-screens --update-snapshots
# 변경 후 서버: 기준 갱신 없이 비교
REVIEW_SCREENSHOTS=1 npm run test:e2e
```

픽셀 차이는 0개까지 허용합니다. 스크린샷과 실패 결과는 기본적으로 `/tmp/ai-usage-review/`에 저장됩니다. `REVIEW_SNAPSHOT_DIR`, `REVIEW_OUTPUT_DIR`, `REVIEW_BASE_URL`, `REVIEW_CHROMIUM_EXECUTABLE`로 경로를 지정할 수 있습니다. 기준 생성 후에는 `--update-snapshots`를 사용하지 않습니다. 기존 DB를 유지한 API 비교는 `review-fixture.py --compare-api /tmp/review-api-before.json`에 같은 `--db`/`--downloads` 인자를 주어 실행합니다.

## CI

GitHub CI는 Go 경쟁 상태 검사, 웹 단위 테스트/빌드, 샘플 DB를 쓰는 브라우저 테스트, Windows PowerShell 설치 검증을 수행합니다. GitLab Go 테스트 이미지에는 설치 검증용 `curl`을 설치합니다. 브라우저 실패 자료는 GitHub Actions 아티팩트로 보존합니다.
