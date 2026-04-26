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

## Go Constraints

- 복잡도 ≤ 15 (`gocyclo`/`gocognit`), 라인 60 초과 시 분리 검토(의미 없는 분리는 금지)
- 중첩 ≤ 3, Guard Clauses (early return). `if-else` 체인 대신 `switch`/early return
- ID는 Phantom Type (`type UserID int64`), 단순 `int64` 금지
- `any`/`interface{}` 사용 시 사유 주석 필수
- 에러 래핑 `fmt.Errorf("ctx: %w", err)`, 검사 `errors.Is`/`As`. 런타임 `panic` 금지
- 모든 I/O 함수 첫 파라미터 `ctx context.Context`. `context.TODO()` 머지 전 제거
- 인터페이스: 사용처(consumer) 정의, 메서드 ≤ 3. Accept interfaces, return structs
- goroutine은 `ctx` 취소 또는 `done` 가드 필수. 공유 상태는 mutex/채널 중 하나로 통일
- 약어 케이스 일관 (`userID`, `httpClient`)
- `golangci-lint` CI 필수

## DB (sqlc)

- **`db/*.sql.go`는 sqlc 자동 생성 — 직접 수정 금지**
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
- 우회 금지: `http.DefaultClient` / `http.Get` / `sql.Open` 직접 사용 시 trace 누락

**SDK auth transport** — `google.golang.org/api/option.WithHTTPClient` 지정 시 라이브러리는 `WithAPIKey`/`WithCredentials`/`WithTokenSource`를 **모두 무시**. base transport `nil`인 `whataphttpx.Client()`로 감싸면 403 발생. 다음 두 패턴만:
- OAuth2/토큰 SDK: `whataphttpx.WrapClient(<인증된 클라이언트>)` (Gmail 등)
- API key SDK: `whataphttpx.ClientWithAPIKey(apiKey)` (Gemini 등)

## TypeScript / CSS

- UI 전부 TypeScript (`.ts`/`.tsx`). `.js` 발견 시 즉시 `.ts` 전환
- `any` 금지 — 불가피하면 `unknown` + type guard
- CSS: `px`/`hex` 하드코딩 금지, `rem` 또는 `variables.css` 토큰. BEM (`block__element--modifier`)

## Serena

매 대화 시작 시 가장 먼저 `mcp__serena__activate_project` 호출 (`project_path: /home/jinro/.gemini/message-consolidator`).

## Development Process

- **DB 확인**: `mcp__turso-db__{list_tables,describe_table,query}` 직접 사용
- **Logic-First**: 브라우저 테스트 전 Node.js/Go 스크립트로 논리 검증
- **Bug Fix**: 케이스 커버 테스트 작성/보완 필수. 테스트 없는 fix는 미완성
- **컨텍스트 절약**: 대규모 grep/find/log 분석 (3 query 이상 또는 raw 출력 large 예상)은 `Agent(subagent_type=Explore)` 위임 — main context 오염 방지. Read는 `offset/limit` 명시, Bash는 `| head -N`/`| wc -l` 등으로 출력 절제

## Commands

```bash
make                  # build
go test ./...         # 전체 테스트
bash unit-test.sh     # unit tests only
sqlc generate         # DB 쿼리 코드 재생성
```
