# 17. 테스트 전략 (Testing Strategy)

> Cross-refs: → [03-backend-architecture.md] → [11-handlers-and-api.md] → [13-frontend-architecture.md] → [16-cli-and-tools.md]

---

## 1. 테스트 전략 개요

### 4-축 테스트 모델 (4-Axis Testing Model)

이 프로젝트는 성격이 다른 네 가지 테스트 축을 병렬로 운영한다.

| 축 (Axis) | 커버리지 대상 | 진입점 | 실행 속도 |
|---|---|---|---|
| **Go 단위 + 통합 + 회귀** | 순수 함수 · DB 계층 · 핸들러 · regression | `go test -tags regression ./...` | 중간 (<30s) |
| **TS 단위 (vitest)** | 프런트엔드 로직 · 렌더러 | `npm test` | 빠름 (<5s) |
| **UI 검증** | 빌드 아티팩트 정적 분석 | `node tests/verify-loading-ui.cjs` | 빠름 (<3s) |
| **AI 회귀 (선택)** | Gemini 분석 결과 안정성 | `make test-ai` | 느림 (VCR) |

`bash unit-test.sh`는 위 3축(Go·NPM·UI)을 **동시(background PID)**로 실행하고 합산 결과를 출력한다. AI 회귀 테스트(`make test-ai`)는 `ai/` 소스 변경이 없으면 자동 건너뛰는 별도 진입점이다. 어느 축 하나라도 실패하면 exit 1.

### WHY: AI 회귀 테스트가 존재하는 이유

Gemini 모델은 버전 deprecate 또는 내부 가중치 변화로 동일 입력에 다른 결과를 낼 수 있다. 또한 프롬프트 수정(drift)이 의도치 않은 task 추출 변화를 유발한다. 회귀 테스트는 이 두 위험을 **자동으로** 포착한다.

> AI 회귀 테스트는 `unit-test.sh`에서 분리되어 `make test-ai` / `make test-ai-force`로 실행된다 (→ 섹션 4).

### CLAUDE.md Bug Fix 룰

> "Bug Fix: 케이스 커버 테스트 작성/보완 필수. 테스트 없는 fix는 미완성."

버그 수정 PR은 반드시 해당 케이스를 재현하는 테스트를 함께 포함해야 한다. 테스트 없는 수정은 미완성(incomplete)으로 간주하며 머지 불가.

---

## 2. Go 단위 테스트

### 파일 분포 (Distribution)

전체 `*_test.go` 파일 수: **88개** (2026-05-01 기준)

패키지별 분포:

| 패키지 | 파일 수 | 특징 |
|---|---|---|
| `store` | 28 | DB 계층 + in-memory SQLite |
| `services` | 22 | 비즈니스 로직 집중 |
| `scanner` | 9 | 파이프라인 슬라이스 |
| `handlers` | 9 | HTTP 핸들러 테이블 |
| `ai` | 8 | 프롬프트 파서 + VCR |
| `channels` | 5 | 채널 연결 · 필터 |
| `tests` | 2 | regression 패키지 (`ai_regression_test.go`, `gmail_mock_test.go`) |
| 기타 | 4 | `logger`, `internal`, `config`, `auth` |

`store`와 `services` 두 패키지가 전체의 57%를 차지한다. 이는 데이터 계층과 비즈니스 로직이 테스트 밀도가 가장 높아야 한다는 아키텍처 원칙과 일치한다. → [03-backend-architecture.md]

### 테이블 기반 테스트 패턴 (Table-Driven Tests)

표준 Go 관용구를 사용한다. `handlers_users_test.go`의 예:

```go
cases := []struct {
    realName    string
    displayName string
    expected    []string
}{
    {"Jaejin Song", "JJ", []string{"Jaejin Song", "JJ"}},
    {"Jaejin Song", "Jaejin Song", []string{"Jaejin Song"}},
}
for _, tc := range cases { ... }
```

`t.Run()` 서브테스트는 명명된 시나리오가 필요한 경우에 사용한다(`handlers_admin_test.go`의 `TestApplyHotReload`).

### testify 사용 여부

표준 `testing` 패키지 assert(`t.Errorf`, `t.Fatalf`)를 기본으로 쓴다. 일부 통합 테스트에서 `testify/assert`를 사용하지만 강제 사항은 아니다.

### Mock vs 실제 의존성 정책

- **DB**: 항상 실제 libsql in-memory DB (`testutil.SetupTestDB`). mock DB 금지.
- **HTTP 외부 API**: `net/http/httptest.NewRecorder` + `httptest.NewServer` 패턴.
- **AI (Gemini)**: VCR 파일 replay (→ 섹션 4) 또는 서비스 레이어에서 인터페이스 mock (`RegressionMockAI`).

### 진입점

```bash
go test -tags regression ./...  # 단위 + 통합 + regression 통합 실행
bash unit-test.sh               # 3축 병렬 실행 (Go·NPM·UI)
```

---

## 3. Go 통합 테스트

### tests/ 디렉토리 구조

Go unit 테스트와 regression 테스트는 `go test -tags regression ./...` 단일 명령으로 통합 실행된다. 별도 AI 전용 test 바이너리는 제거되었다.

```
tests/
├── logic_verification_test.go  # 루트 레벨 로직 검증
├── mobile_layout.test.ts       # (TS, 5절 참조)
├── regression/
│   ├── ai_regression_test.go   # AI 회귀 (//go:build regression)
│   ├── gmail_mock_test.go      # Gmail 채널 목
│   └── testdata/               # VCR 입력·기대값 (덤프는 로컬 전용, → 섹션 4)
└── verify-loading-ui.cjs       # CSS/HTML 구조 검증 (Node.js)
```

### testutil/db.go — DB 픽스처

경로: [`internal/testutil/db.go`](../../internal/testutil/db.go)

```go
func SetupTestDB(initFunc func(context.Context, *config.Config) error,
                 resetFunc func()) (func(), error) {
    cfg := &config.Config{}  // Why: empty cfg → store.TestDSN 선택
    if err := initFunc(context.Background(), cfg); err != nil {
        return nil, fmt.Errorf("failed to init test database: %w", err)
    }
    return func() { if resetFunc != nil { resetFunc() } }, nil
}
```

**핵심 설계 결정:**

- `modernc.org/sqlite`는 `cache=shared` in-memory를 지원하지 않는다. 각 커넥션이 독립적인 메모리 DB를 갖는다.
- `store.TestDSN`을 주입하면 `store.InitDB`가 `db.SetMaxOpenConns(1)`을 호출해 단일 커넥션을 강제한다 → 모든 goroutine이 동일한 메모리 DB를 공유.
- `RandomEmail`, `RandomTS`, `RandomID` 헬퍼로 테스트 간 unique constraint 충돌을 방지한다.

### 핸들러 통합 테스트 패턴

`handlers_admin_test.go`, `handlers_users_test.go` 모두 `net/http/httptest`를 사용한다:

```go
rr := httptest.NewRecorder()
req, _ := http.NewRequestWithContext(ctx, "POST", "/api/...", body)
handler.ServeHTTP(rr, req)
if rr.Code != http.StatusOK { ... }
```

실제 DB가 연결된 `*API` 인스턴스를 생성해 HTTP 계층 → 서비스 계층 → DB 계층 전체를 통과하는 방식으로 통합 테스트를 수행한다. → [11-handlers-and-api.md]

### 외부 SDK / HTTP 모킹

- Gmail, WhatsApp 등 외부 API는 `httptest.NewServer`로 가짜 엔드포인트를 제공하거나, `tests/regression/gmail_mock_test.go`처럼 별도 mock 파일로 분리한다.
- `gomock`은 현재 사용하지 않는다. 인터페이스 기반 직접 struct mock을 선호한다.

---

## 4. Go 회귀 테스트 (AI)

### 개요와 빌드 태그

AI 회귀 테스트는 `//go:build regression` 태그로 격리된다. 일반 `go test ./...`에서는 실행되지 않으며, 명시적으로 `-tags regression`을 전달해야 한다. 이는 API 비용과 실행 시간을 제어하기 위해서다.

### 진입점 (Makefile)

```makefile
test-ai:
    @BASE=$$(git merge-base HEAD origin/main ...); \
    CHANGED=$$(git diff --name-only $$BASE HEAD -- $(AI_SOURCES)); \
    if [ -n "$$CHANGED" ]; then \
        go test -v -tags regression ./ai/... ./tests/regression/...; \
    fi

test-ai-force:
    go test -v -tags regression ./ai/... ./tests/regression/...
```

`make test-ai`는 `ai/` 소스 변경이 없으면 자동으로 건너뛴다. 강제 실행은 `make test-ai-force`.

**AI_SOURCES** 감시 대상:
```
ai/prompts  ai/prompts.go  ai/gemini.go  ai/executor.go  ai/analyzers.go  ai/rag.go
```

### VCR (Video Cassette Recorder) 메커니즘

경로: [`tests/regression/ai_regression_test.go`](../../tests/regression/ai_regression_test.go)

`vcrTransport`가 `http.RoundTripper`를 구현한다:

- **replay 모드** (기본): `testdata/<name>_vcr.dump` 파일을 읽어 HTTP 응답을 재생한다. API 키 불필요, 네트워크 비용 0.
- **record 모드** (`UPDATE_GOLDEN=1` 또는 VCR 파일 없을 때): 실제 Gemini API 호출 후 덤프 파일을 기록한다.

프롬프트 파일(`.prompt`)의 mtime이 VCR 덤프보다 최신이면 자동으로 record 모드로 전환한다 (`shouldRecord` 함수). 이것이 **prompt drift 감지** 핵심 메커니즘이다.

### testdata 구조

경로: [`tests/regression/testdata/`](../../tests/regression/testdata/)

각 케이스는 3~4개 파일 세트:

| 파일 | 역할 | VCS |
|---|---|---|
| `<name>_input.txt` | Gemini에 전달할 원본 메시지 텍스트 | 추적 |
| `<name>_expected.json` | 기대하는 `[]store.TodoItem` | 추적 |
| `<name>_vcr.dump` | HTTP 응답 바이너리 덤프 (replay용) | **비추적** (`.gitignore`) |
| `<name>_lang.txt` | (선택) 언어 힌트 (`ko`, `id` 등) | 추적 |

`*_vcr.dump` 파일은 `.gitignore`에 등록되어 VCS 비추적 상태로 유지된다. SDK 버전 특이적이며 `UPDATE_GOLDEN=1` 환경 변수 또는 VCR 파일 부재 시 자동 재생성된다.

```bash
# VCR 덤프 재생성 (실제 Gemini API 호출)
UPDATE_GOLDEN=1 go test -v -tags regression ./tests/regression/...
```

현재 케이스 (8개): `01_simple_slack`, `02_indonesian_kita`, `03_no_tasks`, `04_we_pronoun`, `05_id_formats`, `gmail_twin_task`, `negative`, `slack_unrefined`.

### 회귀 검증 흐름

```
TestAnalyze_Regression
  → filepath.Glob("testdata/*_input.txt")  // 케이스 자동 수집
  → t.Parallel()                            // 케이스 동시 실행
  → setupGeminiClientForTest               // VCR transport 주입
  → client.Analyze(ctx, ...)               // 실제 or 재생 API 호출
  → compareResults(t, expected, actual)    // 키워드 포함 여부 검증
```

`compareResults`는 정확한 JSON 일치가 아닌 `taskKeywords` 맵 기반 키워드 포함 검사를 수행한다. 이는 모델이 표현을 약간 바꿔도 의미가 동일하면 통과하도록 하기 위해서다.

### services 레이어 회귀

`services/tasks_regression_test.go`와 `services/consolidation_regression_test.go`는 `RegressionMockAI`를 사용해 네트워크 없이 멀티-턴 대화 시나리오를 검증한다. AI 통합 레이어가 아닌 서비스 로직의 상태 전이(task state machine)를 대상으로 한다.

---

## 5. TS 단위 테스트 (vitest)

### 파일 분포

전체 `*.test.ts` 파일 수: **21개**

```
src/
├── api.test.ts                      # HTTP 클라이언트 + 에러 처리
├── archive.test.ts                  # 아카이브 필터 로직
├── events.test.ts                   # 이벤트 버스
├── i18n.test.ts                     # 번역 키 존재 여부
├── insights.test.ts                 # 인사이트 집계
├── insightsRenderer.test.ts         # 렌더러 출력
├── logic.test.ts                    # 핵심 비즈니스 로직
├── logic.report.test.ts             # 리포트 로직
├── modals.test.ts                   # 모달 DOM 상태
├── renderer.test.ts                 # 메시지 렌더링
├── state.test.ts                    # 전역 상태 관리
├── state_sync.test.ts               # 상태 동기화
├── utils.test.ts                    # 유틸리티 함수
├── components/
│   └── message-card.test.ts         # 컴포넌트 단위
├── tests/
│   ├── logic_count.test.ts          # 카운트 로직
│   ├── logic_data.test.ts           # 데이터 변환
│   ├── logic_filter.test.ts         # 필터 로직
│   ├── logic_format.test.ts         # 포맷 함수
│   ├── task_filter.test.ts          # 태스크 필터
│   └── translation_logic.test.ts    # 번역 로직
└── utils/
    └── http.test.ts                 # HTTP 유틸
```

`tests/mobile_layout.test.ts`도 `tests/` 루트에 존재한다 (총 21개).

### vitest 설정

경로: [`vitest.config.ts`](../../vitest.config.ts)

```typescript
export default defineConfig({
  test: {
    // Why: Default to node — happy-dom boot ~1.5s per file × 16 non-DOM files = wasted ~24s cumulative.
    // The 6 DOM-needing files override per-file via `// @vitest-environment happy-dom` directive.
    environment: 'node',
    globals: true,
    setupFiles: ['./src/tests/setup.ts'],
  },
});
```

기본 환경을 `node`로 변경한 이유: DOM이 불필요한 파일(16개)에서 `happy-dom` 부트스트랩 비용(`~1.5s × 16 ≈ 24s`)을 제거하기 위해서다. DOM API가 필요한 파일(6개)은 파일 상단에 `// @vitest-environment happy-dom` 디렉티브를 명시해 개별 지정한다.

```typescript
// @vitest-environment happy-dom
// (파일 상단에 선언 — archive.test.ts, modals.test.ts, renderer.test.ts 등 6개)
```

### 주요 테스트 대상과 WHY

**`src/logic.test.ts`** — 상태 없이 순수하게 테스트 가능한 비즈니스 로직 함수를 집중 검증한다. 함수 단위 격리가 가능한 Clean Architecture의 이점을 직접 활용. → [13-frontend-architecture.md]

**`src/api.test.ts`** — 실제 fetch를 `vi.fn()`으로 mock해 네트워크 없이 에러 핸들링(4xx, 5xx, 네트워크 실패)을 검증한다.

**`src/renderer.test.ts`** — `happy-dom` 환경에서 DOM 결과물을 `innerHTML`로 검사한다. CSS 클래스와 데이터 속성 존재 여부를 어서트.

**`src/state.test.ts`** — 전역 상태 변이 함수가 예상 부수효과를 일으키는지 검증한다.

**`src/tests/logic_*.test.ts`** — `logic.ts`의 대규모 분해 후 도메인별로 분리된 테스트 모음. count/data/filter/format 네 관점으로 나눠 테스트 가독성을 유지한다.

### 실행

```bash
npm test               # vitest run (단발 실행)
npm run test:watch     # 감시 모드 (개발 중)
```

---

## 6. 검증 도구 (verify/)

`tests/verify-loading-ui.cjs` (빌드 아티팩트 정적 분석)와 `src/tests/verify-logic.ts` (로직 검증 스크립트)는 `bash unit-test.sh`의 4번째 병렬 태스크로 실행됩니다. CI 미포함 — 로컬 전용. `cmd/verify/*` Go 검증 도구 카탈로그 → [16-cli-and-tools.md §16.3](16-cli-and-tools.md).

---

## 7. CI (GitHub Actions)

### 현재 자동화 범위

경로: [`.github/workflows/lint.yml`](../../.github/workflows/lint.yml)

```yaml
on:
  pull_request:
    paths: ["**.go", "go.mod", "go.sum", ".golangci.yml"]
  push:
    branches: [main]
```

`golangci-lint v1.64.8`만 자동 실행된다. Go 1.25, `--timeout=5m` 옵션.

### 테스트 자동화 현황

| 항목 | CI 포함 여부 |
|---|---|
| `golangci-lint` | **예** (lint.yml) |
| `go test ./...` | 아니오 (수동) |
| `npm test` (vitest) | 아니오 (수동) |
| `make test-ai` (AI 회귀) | 아니오 (수동) |
| `verify-loading-ui.cjs` | 아니오 (수동) |

WHY 미포함: GCP e2-micro 인프라에서 Gemini API 키를 CI secret으로 관리하는 비용 대비 효과가 현재는 음수다. `make test-ai`는 AI_SOURCES 변경 여부를 스스로 판단해 건너뛰므로 개발자 로컬 실행으로 충분하다.

**Delta**: 단위 테스트(`go test ./...`, `npm test`)는 CI에 추가할 수 있다. API 키 없이 실행 가능하고 실행 시간이 짧다.

---

## 8. Cross-References + Deltas

### Cross-References

| 참조 챕터 | 연관 내용 |
|---|---|
| → [03-backend-architecture.md] | Handler → Service → Store 의존성 방향이 테스트 격리 전략 결정 |
| → [11-handlers-and-api.md] | httptest 기반 핸들러 통합 테스트 대상 |
| → [13-frontend-architecture.md] | Clean Architecture → vitest 순수 함수 테스트 가능 이유 |
| → [16-cli-and-tools.md] | `mc-util`, `verify-logic.ts` 등 CLI 도구 검증 방법 |

### Recent Changes (변경 이력)

| 항목 | 이전 | 현재 |
|---|---|---|
| Go 커버리지 | 36.9% | 40.0% |
| VCR fixtures | git 추적 | `.gitignore` (`*_vcr.dump`) 비추적 |
| vitest 기본 환경 | `happy-dom` | `node` (DOM 필요 파일만 개별 `happy-dom`) |
| 테스트 진입점 구조 | unit · regression · AI 분리 | Go unit + regression 통합, AI는 `make test-ai` 별도 |
| AI 회귀 케이스 수 | 6개 | 8개 (`negative`, `slack_unrefined` 추가) |

### Known Deltas (미해결 사항)

1. **CI 테스트 미자동화**: `go test ./...`와 `npm test`가 CI workflow에 없다. lint 단독으로는 논리 오류를 잡지 못한다.
2. **회귀 VCR 케이스 확장**: 현재 8개 케이스. WhatsApp/Telegram 채널용 케이스 부재.
3. **핸들러 커버리지 갭**: `handlers/` 9개 파일 중 `handlers_gmail.go`, `handlers_whatsapp.go`, `handlers_stats.go` 등의 통합 테스트가 얕다.
4. **store 테스트 DB 전략**: `store.TestDSN` + `SetMaxOpenConns(1)` 방식은 병렬 테스트 패키지 간 DB 상태 공유를 전제한다. 패키지 간 격리가 필요하면 `ResetForTest`를 각 테스트 함수에서 명시적으로 호출해야 한다.

---

## Summary

- **`*_test.go` 총 파일 수**: 88개
- **`*.test.ts` 총 파일 수**: 21개
- **Go 커버리지**: 40.0%
- **AI 회귀 testdata 위치**: [`tests/regression/testdata/`](../../tests/regression/testdata/) (케이스 8개, `*_vcr.dump`는 로컬 전용)
- **단위 테스트 스크립트**: [`unit-test.sh`](../../unit-test.sh) — 3축 병렬 실행 (Go·NPM·UI), 단일 exit code
- **CI**: lint 전용 ([`.github/workflows/lint.yml`](../../.github/workflows/lint.yml)), 테스트 자동화 미포함
