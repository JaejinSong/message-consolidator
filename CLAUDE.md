# [PROJECT: message-consolidator]

## Stack

| Layer | Tech |
|---|---|
| Backend | Go 1.25.6, gorilla/mux, SQLite/Turso, sqlc v2 |
| Integrations | whatsmeow, slack-go, gotd/td (Telegram MTProto) |
| Monitoring | WhaTap APM (`github.com/whatap/go-api/trace`) — manual instrumentation only |
| Frontend | Vite + Vanilla TypeScript (Clean Architecture) |
| Infra | Docker (FE/BE 분리), Caddy, GCP e2-micro |

## Architecture

- 의존성: Handler → Service → Store 단방향
- Store 레이어에 비즈니스 로직 금지

## 문제를 푸는 순서

현재 확인된 요구를 충족하면서 전체 복잡성을 줄이는 안을 먼저 택합니다. 새 규칙이나 예외가 필요하면 전제·구조를 바꿔 없앨 수 있는지 먼저 검토합니다. 판단에는 변경 비용도 포함합니다.

## Go Constraints

- 린터가 강제하는 규칙(복잡도·중첩·에러 래핑·panic·ctx 전파·명명·WhaTap 우회 호출·TS `any`·CSS px/hex)은 [.golangci.yml](.golangci.yml) · [eslint.config.js](eslint.config.js) · [.stylelintrc.json](.stylelintrc.json)이 단일 출처이며 여기에 반복하지 않습니다. CI 필수 ([lint.yml](.github/workflows/lint.yml))
- 훅이 강제하는 규칙(sqlc 산출물 `db/*.go` 수동 편집, `src/`·`static/` 신규 `.js`/`.jsx`)은 [.claude/hooks/guard-edit.sh](.claude/hooks/guard-edit.sh)가 단일 출처입니다. 훅 등록은 개인 `.claude/settings.json`의 `PreToolUse`
- 라인 60 초과 시 분리 검토(의미 없는 분리는 금지). Guard Clauses (early return), `if-else` 체인 대신 `switch`/early return
- ID는 Phantom Type (`type UserID int64`), 단순 `int64` 금지
- `any`/`interface{}` 사용 시 사유 주석 필수
- 모든 I/O 함수 첫 파라미터 `ctx context.Context`. `context.TODO()` 머지 전 제거
- 인터페이스: 사용처(consumer) 정의, 메서드 ≤ 3. Accept interfaces, return structs
- goroutine은 `ctx` 취소 또는 `done` 가드 필수. 공유 상태는 mutex/채널 중 하나로 통일

## DB (sqlc)

- 흐름: `store/queries/*.sql` 편집 → `sqlc generate` → `db/*.sql.go` 갱신
- 쿼리는 sqlc 우선, raw SQL은 동적 IN절 등 정적 분석 불가 케이스에 한정
- `sqlc generate` 후 의도치 않게 변경된 파일은 `git checkout <file>` 원복

## WhaTap APM

- **Manual only** — 자동 도구(`whatap-go-inst`) 금지, 빌드는 `go build`
- **부트스트랩**: [main.go](main.go) 진입부 `trace.Init(map[string]string{})` + `defer trace.Shutdown()`. 누락 시 모든 trace 호출 silent no-op
- 공식 예제: https://github.com/whatap/go-api-example

| 영역 | 패턴 |
|---|---|
| HTTP in | `r.Use(WhatapMiddleware)` — [middleware_whatap.go](handlers/middleware_whatap.go) |
| HTTP out | `whataphttp.HttpGet(ctx, url)` or `Transport: whataphttp.NewRoundTrip(ctx, nil)` |
| SQL/sqlc | `whatapsql.OpenContext(ctx, driver, dsn)` — [store/db.go](store/db.go) |
| Background goroutine | `trace.Start(ctx, "/<TxName>")` + `defer trace.End(ctx, err)` |
| 함수 단위 | `method.Start(ctx, name)` + `defer method.End(methodCtx, err)` |
| 외부 SDK step | `trace.Step(ctx, name, "", elapsedMs, value)` |
| gRPC | `whatapgrpc.{Unary,Stream}{Client,Server}Interceptor()` |

**Gotcha:**
- Background TX 이름은 `/`로 시작 — `urlutil.NewURL`이 슬래시 없으면 Host로 파싱, Transaction 컬럼 빈 칸
- `StartWithContext` 사용 금지 — 기존 trace ctx 없으면 silent skip. background는 `trace.Start`

**Outbound HTTP wrapping** — OAuth2 (Gmail 등): `whataphttpx.WrapClient(<oauth2.NewClient(...)>)`. 그 외 (Slack/Gemini 등): `whataphttpx.Client()`. Gemini는 `&genai.ClientConfig{APIKey, HTTPClient: whataphttpx.Client()}`로 주입.

## TypeScript / CSS

- 불가피한 타입 우회는 `unknown` + type guard. CSS 값은 `rem` 또는 `variables.css` 토큰, BEM (`block__element--modifier`)

## Serena

매 대화 시작 시 가장 먼저 `mcp__serena__activate_project` 호출 (`project_path: /home/jinro/message-consolidator`).

## Development Process

- **DB 확인**: `mcp__turso-db__{list_tables,describe_table,query}` 직접 사용
- **Logic-First**: 브라우저 테스트 전 Node.js/Go 스크립트로 논리 검증
- **Bug Fix**: 케이스 커버 테스트 작성/보완 필수. 테스트 없는 fix는 미완성
- **컨텍스트 절약**: 대규모 grep/find/log 분석 (3 query 이상 또는 raw 출력 large 예상)은 `Agent(subagent_type=Explore)` 위임 — main context 오염 방지. Read는 `offset/limit` 명시, Bash는 `| head -N`/`| wc -l` 등으로 출력 절제
- **탐색 도구 분기**: 글로벌 `explore_routing` 규칙 적용. 추가 제약: LSP가 못 보는 것(주석 내 식별자·문자열·빌드 산출물)에 Serena 사용 금지

## 모델 위임 — 프로젝트 제약 (글로벌 규칙 상속)

글로벌 `delegation` 블록(`~/.claude/CLAUDE.md`) 기준. 프로젝트 고유 제약만 명시:

- **에이전트 파일**: [~/.claude/agents/implementer.md](~/.claude/agents/implementer.md) · [~/.claude/agents/haiku-implementer.md](~/.claude/agents/haiku-implementer.md) (전역)
- **simple-implementer 추가 gate**: `db/*.sql.go` · `store/queries/*.sql` 접근 시 즉시 stop (sqlc 생성물)
- **architecture review 트리거**: Handler→Service→Store 위반·의존 방향 역전 → Opus

## Commands

```bash
make                  # build
go test ./...         # 전체 테스트
npm run lint          # eslint + stylelint
bash unit-test.sh     # unit tests only
sqlc generate         # DB 쿼리 코드 재생성
```

---

> **종합 문서 (SSOT)**: [docs/comprehensive/INDEX.md](docs/comprehensive/INDEX.md). 본 파일의 운영 룰은 SSOT와 별도로 유지되며, 충돌 시 본 파일 우선 적용 후 SSOT 갱신.
