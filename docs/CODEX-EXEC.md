# Codex 비대화형 사용량 진단

## 확인 결과

2026-11-11, codex-cli 0.167.0과 `gpt-6-luna`로 실제 비대화형 호출을 실행했습니다. `--ephemeral`을 사용한 호출은 정상적으로 토큰을 사용하지만 세션 파일이 없어 현재 에이전트가 집계할 수 없습니다. Luna 모델 식별이나 비대화형 호출 자체를 제외하는 문제가 아닙니다.

| 실행 방식 | CLI가 보고한 입력 / 캐시 읽기 / 출력 | 세션 파일 | 수집 결과 |
|---|---|---|---|
| 일반 `codex exec` | 9,547 / 7,680 / 5 | 생성됨 | `gpt-6-luna` 1건 |
| `codex exec --ephemeral` | 9,547 / 0 / 5 | 생성 안 됨 | 0건 |

입력 토큰은 CLI에서 캐시를 포함합니다. 일반 호출에서 프로젝트의 `scan --json`은 입력 1,867, 캐시 읽기 7,680, 출력 5로 정확히 변환했습니다. 일반 호출 기록 중 세션 메타데이터·모델·토큰 레코드만 임시 디렉터리에 추출해 검사했으며 운영 서버로 전송하지 않았습니다.

표의 두 호출은 아래 명령과 동일한 임시 디렉터리·프롬프트를 사용해 `--ephemeral` 유무만 바꿔 실행했습니다. 사용자가 제공한 번역 명령의 `--output-schema`는 이 최소 응답 검사에서 생략했습니다.

## 재현 명령

빈 임시 디렉터리에서 다음처럼 짧은 응답을 요청합니다. 인증은 기존 CLI 설정을 사용하며 모델 호출에 사용량이 발생합니다.

```sh
codex exec --json --color never \
  --sandbox read-only --skip-git-repo-check \
  --ephemeral --ignore-user-config --ignore-rules \
  --disable shell_tool --disable unified_exec --disable apps \
  --disable plugins --disable hooks --disable multi_agent \
  --disable image_generation --disable browser_use --disable computer_use \
  -c 'web_search="disabled"' \
  -C <빈-임시-디렉터리> -m gpt-6-luna -
```

표준 입력으로 `Respond with exactly OK. Do not use tools, read files, or change anything.`를 전달한 뒤 EOF로 닫습니다. `thread.started.thread_id`를 기록하고 `turn.completed.usage`에 사용량이 있는지 확인합니다. `~/.codex/sessions`에서 해당 스레드 ID를 가진 파일은 생성되지 않습니다.

같은 명령에서 `--ephemeral`만 제거하면 해당 스레드 ID의 rollout 파일이 생성됩니다. 에이전트가 그 세션 디렉터리를 읽도록 설정되어 있으면 다음 명령으로 확인할 수 있습니다. `scan`은 서버로 전송하거나 수집 커서를 변경하지 않습니다.

```sh
ai-usage-agent scan --source codex --since 1h --json
```

실제 모델 호출 없이 재현 형식과 토큰 변환, 재스캔 중복 방지를 확인하는 테스트:

```sh
go test ./internal/collector/codex -run TestExecLunaCollection -v
```

고정된 익명화 레코드를 사용하며 실제 인증이나 사용자 로그는 읽지 않습니다. 테스트는 일반 rollout 수집, 세션 파일 부재, CLI 표준 출력 형식의 미지원 세 가지를 검사합니다. `--ephemeral`의 파일 미생성 동작 자체는 위 실제 CLI 호출로 확인했습니다.

## 브리지에서 집계하려면

- 세션 기록을 보관해도 된다면 브리지 호출에서 `--ephemeral`을 제거합니다. 이후 호출은 기존 수집기로 처리할 수 있습니다. 이 경우 번역 원문·응답 등도 Codex 세션 파일에 남을 수 있습니다.
- `--ephemeral`을 유지하려면 브리지가 이미 읽는 JSON 스트림에서 `turn.completed.usage`와 스레드 ID, 시각, 요청 모델을 추출해 사용량 전용 기록으로 남기고, 에이전트에 그 형식을 읽는 연동을 추가해야 합니다. 현재 프로젝트에는 이 경로가 구현되어 있지 않습니다. 표준 출력 전체를 세션 폴더에 저장하는 것만으로는 동작하지 않습니다.
- 사용량 기록을 별도로 보관하지 않은 과거 ephemeral 호출은 현재 수집기의 재스캔으로 복구할 수 없습니다.

공식 [OpenAI Docs의 비대화형 실행 설명](https://learn.chatgpt.com/docs/non-interactive-mode)은 `--ephemeral`의 세션 파일 미저장 동작과 `--json`의 `turn.completed.usage` 형식을 명시합니다.
