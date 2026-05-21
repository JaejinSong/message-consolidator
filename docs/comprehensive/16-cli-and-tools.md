# 16. CLI 및 도구 (CLI and Tools)

> 이 챕터는 `cmd/` 하위 11개 CLI 바이너리(또는 서브커맨드)를 분류하고, 각 도구의 목적·입력·출력·사용 시점을 설명합니다.

---

## 16.1 CLI 카탈로그 개요 (CLI Catalog Overview)

`cmd/` 디렉토리에는 메인 서버 바이너리와 별개로 4가지 카테고리의 단독 실행 도구가 있습니다:

| 카테고리 | 디렉토리 | 도구 수 | 역할 |
|---|---|---|---|
| **시뮬레이터 (sim_*)** | `cmd/sim_*` | 4 | 스케줄 없이 리포트·분석 파이프라인을 즉시 실행 |
| **검증 (verify/*)** | `cmd/verify/*` | 4 | 스토어·AI 로직의 회귀 검증 (in-memory DB 격리) |
| **점검 (check-*)** | `cmd/check-*`, `cmd/reset-*` | 3 | 외부 서비스(Gemini, Slack, Gmail) 토큰·체크포인트 상태 확인 |
| **운영 (mc-util)** | `cmd/mc-util` | 1 binary / 4 subcmd | DB 진단, 태스크 정규화, WhatsApp 페어링, 릴리즈 노트 생성 |
| **실험 (thinking-exp)** | `cmd/thinking-exp` | 1 | Gemini thinking 토큰이 입력 크기·프롬프트 복잡도에 따라 어떻게 스케일하는지 측정 |

### 왜 별도 바이너리인가? (Why separate binaries?)

메인 서버는 장기 실행 데몬이므로 스케줄러 트리거 없이 특정 기능을 한 번만 실행하거나, 운영 환경을 건드리지 않고 격리된 로직을 검증할 수 없습니다. 별도 바이너리는 다음을 가능하게 합니다:

- **디버깅 격리**: 메인 서버 재시작 없이 단일 기능 실행
- **스케줄 우회**: `sim_*` 도구로 일일/주간 리포트를 즉시 트리거 (시간대 조건 무시)
- **in-memory DB 격리**: `verify/*`는 실제 Turso DB를 건드리지 않음
- **운영 원샷 작업**: `mc-util`처럼 스케줄이 없는 관리 작업은 서버 코드에 두기 부적절

---

## 16.2 시뮬레이터 (Simulators — sim_*)

시뮬레이터는 스케줄러 없이 서비스 레이어의 `Dispatch()` 함수를 직접 호출합니다. 스케줄 조건(시간, 요일)을 무시하고 실행되므로 리포트 파이프라인 개발·디버깅에 사용합니다.

→ [06-scanner-pipeline.md](06-scanner-pipeline.md) (메시지 수집 파이프라인)
→ [08-services-business-logic.md](08-services-business-logic.md) (DailyDigestService, WeeklyReportService)

### 16.2.1 sim_daily — 일일 요약 시뮬레이터

**파일**: [`cmd/sim_daily/main.go`](../../cmd/sim_daily/main.go)

**목적**: `DailyDigestService.Dispatch()`를 즉시 호출하여 Slack DM으로 일일 요약을 전송합니다. 수신자·시간대·언어 설정이 정상 동작하는지 스케줄러 없이 검증할 때 사용합니다.

**입력 (환경 변수)**:

| 변수 | 필수 | 설명 |
|---|---|---|
| `GEMINI_API_KEY` | 필수 | Gemini AI 클라이언트 인증 |
| `SLACK_TOKEN` | 필수 | Slack DM 발송 |
| `SIM_DAILY_RECIPIENT` | 선택 | 수신자 이메일 override (미지정 시 `DAILY_DIGEST_RECIPIENT_EMAIL` 사용) |

**출력**: Slack DM (수신자 인박스), 콘솔 `[SIM-DAILY] done` 로그

**사용 시점**: 일일 요약 프롬프트·번역·Slack Block Kit 레이아웃 변경 후 즉시 확인

```bash
SIM_DAILY_RECIPIENT=me@example.com go run ./cmd/sim_daily/
```

**구조**:
```
config → DB init → GeminiClient → TranslationService
→ FlashSingleSummarizer → ReportsService
→ DailyDigestService.Dispatch()
```

`SIM_DAILY_RECIPIENT`가 있으면 `DailyDigestConfig.RecipientEmails`를 단일 이메일로 덮어씁니다. 이는 운영 수신자 목록 전체에 테스트 메시지가 발송되는 것을 막기 위한 안전 장치입니다.

---

### 16.2.2 sim_weekly — 주간 보고서 시뮬레이터

**파일**: [`cmd/sim_weekly/main.go`](../../cmd/sim_weekly/main.go)

**목적**: `WeeklyReportService.Dispatch()`를 즉시 호출합니다. Slack DM 발송과 Notion 페이지 내보내기를 동시에 검증합니다.

**입력**:

| 변수 | 필수 | 설명 |
|---|---|---|
| `GEMINI_API_KEY` | 필수 | |
| `SLACK_TOKEN` | 필수 | |
| `NOTION_TOKEN` | 필수 | Notion 내보내기 |
| `SIM_WEEKLY_RECIPIENT` | 선택 | CSV 형식 다중 수신자 override |

`SIM_WEEKLY_RECIPIENT`는 콤마 구분 CSV를 수신자 슬라이스로 파싱합니다:
```go
recipients = splitCSV(os.Getenv("SIM_WEEKLY_RECIPIENT"))
```

**출력**: Slack DM + Notion 페이지 (설정된 `NOTION_REPORT_PAGE_ID` 하위)

**사용 시점**: 주간 리포트 포맷 변경, Notion 내보내기 통합 검증, 다중 수신자 동작 확인

```bash
SIM_WEEKLY_RECIPIENT=a@ex.com,b@ex.com go run ./cmd/sim_weekly/
```

---

### 16.2.3 sim_d — 소화(Digest) 프롬프트 A/B 시뮬레이터

**파일**: [`cmd/sim_d/main.go`](../../cmd/sim_d/main.go)

**목적**: Gemini `ChatAnalyzer` 프롬프트의 두 변형(BASELINE vs D VARIANT)을 동일 페이로드에 대해 N회 반복 실행하여 AI 출력 일관성을 비교합니다. 전달된(forwarded) 메시지 처리 방식이 프롬프트 변경 후에도 정확한지 검증하는 데 사용합니다.

**입력**:

- `GEMINI_API_KEY` 환경 변수 (`.env` 파일에서도 로드)
- 소스 코드 내 하드코딩된 두 페이로드 (`baselinePayload`, `dPayload`)

**출력**: 콘솔 JSON (각 실행 결과 3회)

```
=== BASELINE (current behavior) ===
[run 1] { ... }
...
=== D VARIANT (forwarded relabeled + system addendum) ===
[run 1] { ... }
```

**사용 시점**: AI 프롬프트 시스템 인스트럭션 수정 시, 특히 forwarded 메시지 `source_ts` 추출 로직 변경 후 회귀 확인. → [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md)

메인 서버의 `store.InitDB`나 Slack 토큰이 불필요하므로, 운영 설정 없이 순수 AI 레이어만 실행할 수 있습니다.

---

### 16.2.4 sim_normalize — 이름 정규화 시뮬레이터

**파일**: [`cmd/sim_normalize/main.go`](../../cmd/sim_normalize/main.go)

**목적**: `store.NormalizeName()`과 `store.ResolveAlias()`의 실제 Turso DB 동작을 검증합니다. 채팅 메시지에서 수집된 발신자 이름이 사용자 ID로 올바르게 해석되는지 확인합니다.

**입력**:

- `TURSO_DATABASE_URL` 필수
- 소스 코드 내 하드코딩된 테스트 케이스 (alias, 한국어 이름, 이메일 등)

**출력**: 콘솔 매핑 결과

```
NormalizeName("shared"         ) = "..."
NormalizeName("sunpho"         ) = "..."
ResolveAlias(name, "shared") → id=3, err=nil
```

**사용 시점**: 연락처 별칭(alias) 테이블 변경 후 정규화 로직 회귀 확인. → [09-identity-and-dedup.md](09-identity-and-dedup.md)

`verify/idempotency`와 달리 실제 Turso DB를 사용합니다. 개발 환경에서만 실행해야 합니다.

---

## 16.3 검증 (Verification — verify/*)

`verify/*` 도구는 in-memory SQLite DB(`file:memdb_verify?mode=memory&cache=shared`) 또는 `testutil.SetupTestDB`를 사용하여 실제 DB를 건드리지 않습니다. CI 파이프라인 외부에서 특정 로직 경로를 빠르게 확인할 때 유용합니다.

→ [17-testing-strategy.md](17-testing-strategy.md) (테스트 전략과의 관계)

---

### 16.3.1 verify/idempotency — 사용자 멱등성 검증

**파일**: [`cmd/verify/idempotency/main.go`](../../cmd/verify/idempotency/main.go)

**목적**: `store.GetOrCreateUser()`의 UPSERT가 동일 이메일에 대해 항상 같은 `UserID`를 반환하고, 이름은 최신 값으로 갱신되는지 확인합니다.

**검증 시나리오**:

1. 동일 이메일로 두 번 호출 → ID 동일 여부
2. 두 번째 호출의 `Name` 필드가 우선 적용되는지

```go
u1, _ := store.GetOrCreateUser(ctx, email, "Name 1", "Pic 1")
u2, _ := store.GetOrCreateUser(ctx, email, "Name 2", "Pic 2")
// u1.ID == u2.ID && u2.Name == "Name 2"
```

**DB**: in-memory (`file:memdb_verify?mode=memory&cache=shared`) — Turso 불필요

**사용 시점**: `store.GetOrCreateUser` 쿼리 수정, UPSERT 전략 변경 후

---

### 16.3.2 verify/batch_partial — 부분 배치 번역 검증

**파일**: [`cmd/verify/batch_partial/main.go`](../../cmd/verify/batch_partial/main.go)

**목적**: 번역 배치에서 일부 항목이 실패(오류)했을 때 성공한 항목만 DB에 저장되고, 실패 항목은 캐시에 절대 기록되지 않음을 검증합니다.

**검증 시나리오**:

1. AI가 ID 1·3은 성공, ID 2는 오류를 반환하는 상황을 모의
2. 성공 항목만 `SaveTaskTranslationsBulk` 호출
3. `GetTaskTranslationsBatch`로 ID 2가 캐시에 없음을 확인

```go
// ID 2는 캐시에 있으면 안 됨
if _, ok := cached[2]; ok {
    log.Fatal("ERROR: ID 2 should NOT be in the cache")
}
```

**사용 시점**: 번역 파이프라인의 오류 처리 로직 변경 후 부분 실패 모델 회귀 확인

---

### 16.3.3 verify/translation_batch_cost — 번역 비용 최적화 검증

**파일**: [`cmd/verify/translation_batch_cost/main.go`](../../cmd/verify/translation_batch_cost/main.go)

**목적**: 번역 캐시 UPSERT(`SaveTaskTranslationsBulk`)가 중복 호출 시 기존 항목을 덮어쓰고, 전체 캐시 정합성이 유지되는지 확인합니다. 이중 번역 요청으로 인한 비용 낭비 방지를 목적으로 합니다.

**검증 시나리오**:

1. 초기 캐시 비어 있음 확인
2. 3건 bulk save
3. 두 번째 조회에서 3건 확인
4. ID 1을 수정된 텍스트로 upsert 후 갱신 확인

**사용 시점**: `store.SaveTaskTranslationsBulk`의 UPSERT 쿼리 변경 후

---

### 16.3.4 verify/double_injection — 이중 이메일 주입 방지 검증

**파일**: [`cmd/verify/double_injection/main.go`](../../cmd/verify/double_injection/main.go)

**목적**: `auth.AuthMiddleware`가 쿼리스트링에 `email` 파라미터를 주입할 때, 동일 파라미터가 이미 존재하거나 다른 파라미터가 혼재하는 경우에도 `email`이 정확히 1개만 존재하는지 확인합니다.

**검증 시나리오**:

| 케이스 | 초기 쿼리 | 기대 email 수 |
|---|---|---|
| 파라미터 없음 | `""` | 1 |
| 이미 주입됨 | `email=test@...` | 1 |
| 다른 파라미터 존재 | `lang=ko&status=done` | 1 |

**사용 시점**: `auth.AuthMiddleware` 쿼리 주입 로직 변경 후 중복 주입 회귀 확인

---

## 16.4 점검 도구 (Check Tools — check-*)

외부 서비스(Gemini API, Slack, Gmail)와의 연결 상태·권한 범위를 사전 확인하는 운영 전용 도구입니다. 환경 변수나 `.env` 파일에서 토큰을 로드하므로, CI 환경보다는 개발자 로컬 또는 VPS에서 직접 실행합니다.

---

### 16.4.1 check-models — Gemini 모델 가용성 확인

**파일**: [`cmd/check-models/main.go`](../../cmd/check-models/main.go)

**목적**: `GEMINI_API_KEY`로 `genai.Client.ListModels()`를 호출하여 현재 키로 접근 가능한 모델 목록을 출력합니다. 새 모델 이름 (`GeminiAnalysisModel`, `GeminiTranslationModel`)을 설정에 추가하기 전에 실제 가용 여부를 확인하는 데 사용합니다.

**입력**: `GEMINI_API_KEY` (`.env` 자동 로드)

**출력**:
```
Available Models:
- models/gemini-3-flash-preview (DisplayName: Gemini 3 Flash Preview)
- models/gemini-2.0-pro-exp (DisplayName: ...)
```

**사용 시점**: 모델명 변경 전, 새 Gemini 모델 출시 후 키 권한 확인

```bash
go run ./cmd/check-models/
```

---

### 16.4.2 check-slack-scope — Slack 토큰 범위 확인

**파일**: [`cmd/check-slack-scope/main.go`](../../cmd/check-slack-scope/main.go)

**목적**: `SLACK_TOKEN`이 DM 발송(`chat:write`) 권한을 보유하는지 확인합니다. `auth.test`로 봇 정보를 조회하고, raw HTTP로 `X-OAuth-Scopes` 헤더를 읽어 실제 부여된 스코프를 출력합니다.

`slack-go` SDK가 응답 헤더를 노출하지 않아, 스코프 확인을 위해 동일 엔드포인트를 raw HTTP로 한 번 더 호출합니다.

**플래그**:

| 플래그 | 설명 |
|---|---|
| `-to <user_id>` | 실제 DM 발송 대상 Slack 사용자 ID (생략 시 봇 자신에게 발송) |
| `-dry` | PostMessage 생략, auth.test + 스코프 확인만 수행 |

**출력**:
```
[auth.test] team="Foo" user="mc-bot" user_id=U123 bot_id=B456
[scopes] chat:write,im:write,users:read,...
[OK] chat:write present
[OK] DM delivered: channel=D789 ts=1234567890.123
```

**사용 시점**: Slack 앱 매니페스트 변경 후 재설치, 신규 환경 배포 시 권한 확인

```bash
go run ./cmd/check-slack-scope/ -to U01ABCD1234
go run ./cmd/check-slack-scope/ -dry   # DM 없이 스코프만 확인
```

---

### 16.4.3 reset-gmail-checkpoint — Gmail 스캔 체크포인트 초기화

**파일**: [`cmd/reset-gmail-checkpoint/main.go`](../../cmd/reset-gmail-checkpoint/main.go)

**목적**: `scan_metadata` 테이블의 Gmail inbox 체크포인트를 지정 타임스탬프로 재설정합니다. 과거 메시지를 재처리하거나, 체크포인트가 너무 최신으로 이동해 메시지를 놓쳤을 때 롤백하는 데 사용합니다.

**주의**: DB URL과 인증 토큰, 대상 이메일·타임스탬프가 소스 코드에 하드코딩되어 있습니다. 실행 전 값을 확인하고 필요 시 수정해야 합니다.

```go
newTS := "1773878400"  // 2026-03-20 00:00:00 UTC
```

**출력**: `Successfully reset checkpoint for jjsong@whatap.io to 1773878400`

**사용 시점**: Gmail 스캐너가 특정 날짜 이전 메시지를 다시 처리해야 할 때 (운영 원샷, 빈도 낮음)

→ [06-scanner-pipeline.md](06-scanner-pipeline.md) (scan_metadata 역할)

---

## 16.5 운영 도구 (Operations — mc-util)

**파일**: [`cmd/mc-util/main.go`](../../cmd/mc-util/main.go)

`mc-util`은 4개의 서브커맨드를 가진 단일 바이너리입니다. 서브커맨드들은 모두 운영 환경에서 주기적으로 실행할 성격이 아니라 필요 시 수동으로 실행합니다.

```
Usage: mc-util <command> [args]
Commands:
  db-diag       : Database diagnostics (total counts, samples)
  wa-pair       : WhatsApp CLI pairing tool
  release-notes : Generate synchronized release notes
  dedup-tasks   : Remove duplicate [Update:] sections from task fields
```

`make build-mc-util`로 별도 바이너리를 빌드합니다 (메인 바이너리와 분리):

```bash
make build-mc-util   # CGO_ENABLED=0 + UPX 압축
./mc-util db-diag
```

---

### 16.5.1 mc-util db-diag — DB 진단

**파일**: [`cmd/mc-util/db_diag.go`](../../cmd/mc-util/db_diag.go)

**목적**: 사용자별 메시지 통계(전체·완료·삭제)와 `completed_at` 컬럼의 물리 저장 형식을 출력합니다. 배포 후 데이터 이상 여부를 빠르게 확인하는 데 사용합니다.

`DEFAULT_USER_EMAIL` 환경 변수로 조회 대상을 지정합니다. 해당 이메일에 데이터가 없으면 DB 내 전체 이메일 목록을 출력하여 설정 오타를 진단합니다.

**출력**:
```
--- Bin-Diag for user: jjsong@whatap.io ---
Total: 142 | Done (Active): 37 | Done (Total): 41 | Deleted: 12
Sample completed_at: '2026-04-30T14:23:00Z'
strftime('%H', sample): '14'
```

---

### 16.5.2 mc-util wa-pair — WhatsApp CLI 페어링

**파일**: [`cmd/mc-util/wa_pair.go`](../../cmd/mc-util/wa_pair.go)

**목적**: VPS 환경(헤드리스)에서 WhatsApp QR 코드 페어링을 수행합니다. QR 코드를 `whatsapp_qr.png`로 저장하고, 사용자가 모바일 앱으로 스캔할 때까지 대기합니다.

기존 JID 체크를 우회하기 위해 `FetchUserWAJID`를 빈 문자열을 반환하도록 오버라이드합니다:

```go
channels.DefaultWAManager.FetchUserWAJID = func(email string) (string, error) {
    return "", nil // fresh pairing 강제
}
```

**사용 시점**: WhatsApp 세션 만료 후 재페어링, 신규 서버 초기 설정

```bash
./mc-util wa-pair
# → whatsapp_qr.png 생성 후 스캔 대기
```

---

### 16.5.3 mc-util release-notes — 릴리즈 노트 자동 생성

**파일**: [`cmd/mc-util/release_notes.go`](../../cmd/mc-util/release_notes.go)

**목적**: 마지막 릴리즈 노트 이후의 git commit 메시지를 Gemini AI로 분석하여 4가지 형식(기술/사용자 × EN/KO)의 릴리즈 노트를 자동 생성합니다.

**처리 흐름**:

1. `git describe --tags`와 `RELEASE_NOTES_USER_KO.md`에서 최신 버전 추출
2. 마지막 릴리즈 날짜 이후의 커밋을 `git log --since=...`로 수집 (최대 2000바이트)
3. Gemini `gemini-3-flash-preview`로 JSON 형식 릴리즈 노트 생성
4. 4개 파일에 prepend (기존 파일은 `.bak` 백업 후 덮어쓰기)

```
RELEASE_NOTES_TECH_EN.md
RELEASE_NOTES_TECH_KO.md
RELEASE_NOTES_USER_EN.md
RELEASE_NOTES_USER_KO.md
```

이미 동일 버전이 파일에 있으면 건너뜁니다 (멱등성 보장).

**사용 시점**: 배포 전 릴리즈 노트 준비

```bash
./mc-util release-notes
```

---

### 16.5.4 mc-util dedup-tasks — 태스크 중복 섹션 제거

**파일**: [`cmd/mc-util/dedup_tasks.go`](../../cmd/mc-util/dedup_tasks.go)

**목적**: `messages.task` 컬럼에 `--- [Update: ...] ---` 섹션이 중복 기록된 경우(동일 내용이 여러 번 append된 버그 흔적) 중복을 제거하고 DB를 정규화합니다.

`task LIKE '%[Update:%[Update:%'` 조건으로 중복 후보를 필터링한 후, 섹션 헤더(`[Update: date] ---`)를 제외한 본문 내용이 동일한 항목을 제거합니다.

```go
// 섹션 헤더를 벗겨낸 본문으로 중복 여부 판단
key := strings.TrimSpace(sectionContent(p))
```

**사용 시점**: task 필드 append 버그가 수정된 후 기존 오염 데이터 정리 (일회성 운영 작업)

---

## 16.6 실험 도구 (Experiment — thinking-exp)

**파일**: [`cmd/thinking-exp/main.go`](../../cmd/thinking-exp/main.go)

**목적:** Gemini thinking 토큰이 입력 크기(태스크 수)와 프롬프트 복잡도에 따라 어떻게 달라지는지를 측정하는 단독 실험 도구입니다. `ai/core` 의 실제 프롬프트와 동일한 페이로드를 사용하여 3가지 입력 규모(minimal ~3건, medium ~25건, full ~50건)에서 thinking 토큰 수와 응답 시간을 측정합니다.

```bash
# 실행
go run ./cmd/thinking-exp/
```

**측정 항목:**

| 필드 | 설명 |
|---|---|
| `inputTokens` | 입력 프롬프트 토큰 수 |
| `outputTokens` | 생성 텍스트 토큰 수 |
| `thinkingTokens` | 내부 thinking 토큰 수 (별도 과금) |
| `elapsed` | Gemini API 응답 시간 |

**주의:** 이 도구는 실제 Gemini API를 호출하므로 `GEMINI_API_KEY` 환경 변수가 필요하고 API 비용이 발생합니다. 프로덕션 코드와 무관한 연구 목적 도구이며, 별도 Makefile 타겟은 없습니다 (`go run` 으로만 실행).

---

## 16.7 빌드 및 실행 (Build & Execution)

### Makefile 타겟

`cmd/sim_*`, `cmd/verify/*`, `cmd/check-*`, `cmd/reset-*`는 Makefile 빌드 타겟이 없습니다. `go run` 으로 직접 실행하거나 필요 시 `go build`로 개별 바이너리를 생성합니다.

`mc-util`만 전용 빌드 타겟이 있습니다:

```makefile
build-mc-util:
    CGO_ENABLED=0 go build -ldflags="-s -w" -o mc-util ./cmd/mc-util
    upx -1 mc-util
```

`build-all` 타겟은 프론트엔드·백엔드(메인 서버)만 포함하며 `mc-util`은 별도로 빌드합니다:

```bash
make build-mc-util   # mc-util 전용 빌드 (UPX 압축 포함)
make build           # FE + BE 병렬 빌드 (mc-util 미포함)
```

### 직접 실행 패턴

```bash
# 시뮬레이터
go run ./cmd/sim_daily/
go run ./cmd/sim_weekly/
go run ./cmd/sim_d/
go run ./cmd/sim_normalize/

# 검증
go run ./cmd/verify/idempotency/
go run ./cmd/verify/batch_partial/
go run ./cmd/verify/translation_batch_cost/
go run ./cmd/verify/double_injection/

# 점검
go run ./cmd/check-models/
go run ./cmd/check-slack-scope/ -to U01ABCD1234
go run ./cmd/reset-gmail-checkpoint/

# 운영
./mc-util db-diag
./mc-util wa-pair
./mc-util release-notes
./mc-util dedup-tasks
```

---

## 16.8 Cross-References & Deltas

| 도구 | 관련 챕터 |
|---|---|
| sim_daily, sim_weekly | → [08-services-business-logic.md](08-services-business-logic.md) (DailyDigestService, WeeklyReportService) |
| sim_d, sim_normalize | → [06-scanner-pipeline.md](06-scanner-pipeline.md), [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md) |
| sim_normalize | → [09-identity-and-dedup.md](09-identity-and-dedup.md) (NormalizeName, ResolveAlias) |
| verify/* | → [17-testing-strategy.md](17-testing-strategy.md) (검증 도구와 유닛 테스트의 경계) |
| reset-gmail-checkpoint | → [06-scanner-pipeline.md](06-scanner-pipeline.md) (scan_metadata 체크포인트) |
| mc-util wa-pair | → [05-channels.md](05-channels.md) (WhatsApp 채널 초기화) |
| thinking-exp | → [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md) (Gemini thinking 토큰·비용 패턴) |

**알려진 미흡 사항**:

- `reset-gmail-checkpoint`: DB URL, 인증 토큰, 이메일, 타임스탬프가 소스 코드에 하드코딩되어 있습니다. 환경 변수 기반으로 전환되지 않은 상태입니다.
- `sim_d`: 테스트 페이로드가 하드코딩되어 있어 다른 대화 시나리오를 추가하려면 소스 수정이 필요합니다.
- `verify/double_injection`: `auth.AuthDisabled = true`와 `DEFAULT_USER_EMAIL`을 소스에서 직접 설정합니다. 환경 변수로 파라미터화되어 있지 않습니다.
