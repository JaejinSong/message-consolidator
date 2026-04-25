# [PROJECT: message-consolidator]

## Stack

| Layer | Tech |
|---|---|
| Backend | Go 1.25.6, gorilla/mux, SQLite/Turso, sqlc v2 |
| Integrations | whatsmeow, slack-go |
| Monitoring | WhaTap APM (`github.com/whatap/go-api/trace`) — **manual instrumentation only**, see policy below |
| Frontend | Vite + Vanilla TypeScript (Clean Architecture) |
| Infra | Docker (FE/BE 완전 분리), Caddy, GCP Cloud Run |

## Directory Structure

```
main.go          — entrypoint
handlers/        — HTTP handlers (라우팅만)
services/        — business logic
store/           — DB access layer (sqlc + raw SQL 예외)
store/queries/   — sqlc .sql 원본 쿼리 파일
db/              — sqlc 생성 코드 (수동 편집 금지)
internal/        — shared utilities
scanner/         — message scanning
channels/        — channel integrations (whatsmeow, slack-go)
auth/            — authentication
src/             — frontend (domain layer / UI layer 분리)
```

---

## Code Constraints

### Go
- 단일 함수 **40라인 이내**
- 중첩 depth **최대 2단계** — Guard Clauses 우선
- ID 파라미터: **Explicit Integer Conversion** (`int64(id)`) 필수

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

### TypeScript
- UI 코드는 **전부 TypeScript** (`.ts` / `.tsx`)
- `.js` 파일 발견 시 `.ts`로 즉시 전환 (테스트 파일 포함)
- `any` 타입 금지 — 불가피한 경우 `unknown` + type guard 사용
- vitest 설정(`setupFiles` 등)도 `.ts` 기준으로 유지

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
