# [PROJECT: message-consolidator]

## Stack

| Layer | Tech |
|---|---|
| Backend | Go 1.25.6, gorilla/mux, SQLite/Turso, sqlc v2 |
| Integrations | whatsmeow, slack-go |
| Monitoring | WhaTap APM (`github.com/whatap/go-api/trace`) — **manual instrumentation only**, see policy below |
| Frontend | Vite + Vanilla TypeScript (Clean Architecture) |
| Infra | Docker (FE/BE 완전 분리), Caddy, GCP Compute Engine (e2-micro) |

## Directory Structure

```
main.go          — entrypoint
handlers/        — HTTP handlers (라우팅만)
services/        — business logic
store/           — DB access layer (sqlc + raw SQL 예외)
store/queries/   — sqlc .sql 원본 쿼리 파일
db/              — sqlc 생성 코드
internal/        — shared utilities
scanner/         — message scanning
channels/        — channel integrations (whatsmeow, slack-go)
auth/            — authentication
src/             — frontend (domain layer / UI layer 분리)
```

---

## Code Constraints

### Go
#### 복잡도
- 순환/인지 복잡도 ≤ 15 (`gocyclo`, `gocognit`)
- 라인 수 60 초과 시 분리 검토 (의미 없는 분리는 금지)

#### 제어 흐름
- 중첩 최대 3단계, Guard Clauses 우선 (early return)
- `if-else` 체인 대신 `switch` 또는 early return

#### 타입
- ID는 Phantom Type (`type UserID int64`), 단순 `int64` 금지
- `any`/`interface{}` 사용 시 사유 주석 필수

#### 에러
- 래핑 필수: `fmt.Errorf("context: %w", err)`
- 검사: `errors.Is` / `errors.As`
- 런타임 경로 `panic` 금지

#### Context
- 모든 I/O 함수 첫 파라미터: `ctx context.Context`
- `context.TODO()` 머지 전 제거

#### 인터페이스
- 사용처(consumer)에 정의, 메서드 3개 이하
- Accept interfaces, return structs

#### 동시성
- goroutine은 `ctx` 취소 또는 `done` 채널 필수
- 공유 상태는 mutex 또는 채널 중 하나로 통일

#### 명명
- 약어 케이스 일관: `userID`, `httpClient`

#### 검증
- `golangci-lint` CI 통과 필수

### DB

> **`db/*.sql.go`는 sqlc 자동 생성 파일 — 절대 직접 수정 금지**
> `sqlc generate` 실행 시 전부 덮어씌워짐

올바른 쿼리 추가/수정 흐름:
```
store/queries/*.sql 편집 → sqlc generate → db/*.sql.go 자동 갱신
```

- 쿼리는 **sqlc 우선** — raw SQL은 동적 IN절 등 sqlc 정적 분석 불가 케이스에 한정
- `sqlc generate` 후 의도하지 않게 변경된 다른 파일은 `git checkout <file>` 으로 원복

### Architecture
- Handler → Service → Store 단방향 의존성
- Store 레이어에 비즈니스 로직 포함 금지

### WhaTap APM Instrumentation

- **Manual only** — 자동 도구(`whatap-go-inst`) 금지, 빌드는 `go build`
- **부트스트랩** — [main.go](main.go) 진입부에 `trace.Init(map[string]string{})` + `defer trace.Shutdown()`. 누락 시 모든 trace 호출이 no-op
- 공식 예제: https://github.com/whatap/go-api-example

| 영역 | 패턴 / 위치 |
|---|---|
| HTTP 인바운드 | `r.Use(WhatapMiddleware)` — [middleware_whatap.go](handlers/middleware_whatap.go) |
| HTTP 아웃바운드 | `whataphttp.HttpGet(ctx, url)` 또는 `Transport: whataphttp.NewRoundTrip(ctx, nil)` |
| SQL/sqlc | `whatapsql.OpenContext(ctx, driver, dsn)` — [store/db.go](store/db.go), 모든 sqlc 쿼리 자동 노출 |
| Background goroutine | `trace.Start(ctx, "/<TxName>")` + `defer trace.End(ctx, err)` — **이름은 `/`로 시작** (urlutil.NewURL이 슬래시 없으면 Host로 파싱해 Transaction 컬럼 빈 칸). **`StartWithContext` 사용 금지** (기존 trace ctx 없으면 silent skip) |
| 함수 단위 추적 | `method.Start(ctx, name)` + `defer method.End(methodCtx, err)` — TX 내부 호출 그래프 형성 |
| 마커형 step (외부 SDK 후 elapsed 기록) | `trace.Step(ctx, name, "", elapsedMs, value)` |
| gRPC | `whatapgrpc.{Unary,Stream}{Client,Server}Interceptor()` |

**우회 금지** — `http.DefaultClient` / `http.Get` / `sql.Open` 직접 사용 시 trace 누락.

**SDK auth transport 주의** — `google.golang.org/api/option.WithHTTPClient`를 지정하면 라이브러리는 `WithAPIKey` / `WithCredentials` / `WithTokenSource`를 **모두 무시**한다. 즉 `whataphttpx.Client()`(베이스 transport `nil`)로 SDK를 감싸면 인증 헤더가 누락되어 403 `Method doesn't allow unregistered callers`가 발생. 다음 두 패턴 중 하나로만 SDK를 래핑할 것:
- **OAuth2/토큰 기반 SDK** — `whataphttpx.WrapClient(<sdk가 만든 인증 클라이언트>)` (예: `oauth2.NewClient`, Gmail). 기존 transport를 base로 보존
- **API key SDK (Gemini 등)** — `whataphttpx.ClientWithAPIKey(apiKey)`. WhaTap RoundTripper 아래에 `x-goog-api-key` 헤더 주입 transport를 깔아 인증 보존

### TypeScript
- UI 코드는 **전부 TypeScript** (`.ts` / `.tsx`)
- `.js` 파일 발견 시 `.ts`로 즉시 전환 (vitest 설정 등 테스트 파일 포함)
- `any` 타입 금지 — 불가피한 경우 `unknown` + type guard 사용

### CSS
- `px`, `hex` 하드코딩 **금지**
- 반드시 `rem` 또는 `variables.css` 토큰 사용
- BEM 패턴 준수 (`block__element--modifier`)

---

## Serena Setup

**매 대화 시작 시 가장 먼저** `mcp__serena__activate_project`를 호출해야 합니다.

```
project_path: /home/jinro/.gemini/message-consolidator
```

다른 `mcp__serena__*` 툴을 사용하기 전에 반드시 activate가 완료되어야 합니다.

---

## Development Process

### Surgical Read
- 파일 전체 조회 금지
- `grep` / `mcp__serena__find_symbol` / `mcp__serena__get_symbols_overview` 로 필요한 심볼만 확인

### DB 확인
- DB 상태 확인이 필요한 경우 **`mcp__turso-db`** MCP 툴을 직접 사용
  - `mcp__turso-db__list_tables` — 테이블 목록 확인
  - `mcp__turso-db__describe_table` — 테이블 스키마 확인
  - `mcp__turso-db__query` — 직접 SQL 조회/검증

### Logic-First
- 브라우저 테스트 전 Node.js / Go 스크립트 기반 논리 검증 우선

### Bug Fix Policy
- 버그 수정 시 해당 케이스를 커버하는 테스트 **반드시** 함께 작성/보완
- 테스트 없는 버그 수정은 미완성으로 간주

### Task Pipeline
```
Scanner → AI Extraction → DB → Dashboard
```
- AI가 메시지 상태(`new` / `update` / `resolve`) 판별
- 각 단계 간 인터페이스 계약 유지

---

## Commands

```bash
make                  # build
go test ./...         # 전체 테스트
bash unit-test.sh     # unit tests only
sqlc generate         # DB 쿼리 코드 재생성
```
