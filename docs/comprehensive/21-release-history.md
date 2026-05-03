# 21. 릴리즈 히스토리 / Release History

> 최종 갱신: 2026-05-03
>
> Cross-references:
> - AI 필터 파이프라인 → [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md)
> - 서비스 비즈니스 로직 → [08-services-business-logic.md](08-services-business-logic.md)
> - 스캐너 파이프라인 → [06-scanner-pipeline.md](06-scanner-pipeline.md)
> - 채널 통합 → [05-channels.md](05-channels.md)
> - ID/중복 제거 → [09-identity-and-dedup.md](09-identity-and-dedup.md)
> - 잠금/동시성 → [10-locking-and-concurrency.md](10-locking-and-concurrency.md)
> - 데이터 레이어 → [04-data-layer.md](04-data-layer.md)
> - 핸들러/API → [11-handlers-and-api.md](11-handlers-and-api.md)
> - 관찰 가능성 → [15-observability.md](15-observability.md)

---

## 1. 릴리즈 정책 / Release Policy

### 문서 분리 구조

릴리즈 노트는 4종으로 분리 관리된다.

| 파일 | 대상 독자 | 언어 | 목적 |
|---|---|---|---|
| `RELEASE_NOTES_USER_KO.md` | 최종 사용자 | 한국어 | 기능 변화를 비기술적 언어로 전달 |
| `RELEASE_NOTES_USER_EN.md` | 최종 사용자 | English | 동일 내용 영문 병기 |
| `RELEASE_NOTES_TECH_KO.md` | 개발자/운영자 | 한국어 | 구현 상세, 아키텍처 변경, 기술 영향 |
| `RELEASE_NOTES_TECH_EN.md` | 개발자/운영자 | English | 동일 내용 영문 병기 |

### 분리 이유

- **사용자 노트**: 마케팅/지원팀이 배포 알림에 그대로 사용할 수 있도록, 구현 세부사항을 배제하고 사용자 가치 중심으로 서술한다.
- **기술 노트**: 코드 리뷰, 인시던트 분석, 마이그레이션 판단 시 참조. `[FEAT]` / `[FIX]` / `[REFACTOR]` / `[STABILITY]` / `[OPTIMIZE]` / `[SYS]` / `[PERF]` / `[UI]` 태그로 분류된다.

### 버전 체계

`v<major>.<minor>.<patch>` (semver). minor 버전 변경은 breaking change 또는 주요 기능 추가를 의미한다. patch는 버그 수정 및 소규모 개선이다.

현재 최신: **v2.4.7** (2026-05-03). 내부 코드 버전은 `main.go` 상단 `Version` 상수로 관리된다.

### 갱신 책임

- 사용자 노트: 제품 오너 또는 AI 생성 → 수동 검토
- 기술 노트: 기능 구현 담당자가 PR 머지 시 동시 갱신
- 이 챕터(21번): Wave 릴리즈 문서 사이클에서 일괄 갱신

---

## 2. 타임라인 (최신순) / Release Timeline

### v2.4.7 — 2026-05-03
**Commit 범위**: `6a40fb3`..`8b3e616` (2026-04-30 ~ 2026-05-03, ~50 커밋)

#### 사용자 관점 (KO)
- **스마트 검색**: 아카이브 메시지에 의미 기반 검색 추가 — 키워드가 없어도 의미로 찾는다. "Smart" 토글 UI 제공
- **Slack DM 태스크 관리**: Slack DM으로 태스크 목록 조회·완료 처리 가능. `/tasks` 슬래시 커맨드 지원
- **담당자 정보 강화**: Slack 태스크에 담당자 정보 포함 — 누가 처리 중인지 DM에서 바로 확인
- **메모 정확도 향상**: self-DM 요청 메모에서 외부 요청자 정보 보존
- **검색 응답 속도**: 벡터 유사도 계산을 DB 서버로 이전 — 응답 데이터 전송량 약 700배 감소

#### User Perspective (EN)
- Semantic Search: hybrid BM25+cosine archive search — find messages by meaning with "Smart" toggle
- Slack DM Task Interface: query and complete tasks via Slack DM; `/tasks` slash command
- Richer Task Context: assignee info now included in Slack task metadata
- Accurate Memos: external requester preserved in self-DM reported-speech notes
- Faster Search: cosine computation offloaded to DB server (~700× less WAN transfer)

#### 기술 상세

**Features**
- `[FEAT]` Hybrid semantic archive search — FTS5 BM25 ∪ cosine(gemini-embedding-001 768d), RRF k=60 퓨전. `/api/messages/archive/semantic` 신규 엔드포인트. 프론트엔드 "Smart" 토글 (en/ko/id/th i18n) (→ [11-handlers-and-api.md](11-handlers-and-api.md), [14-frontend-ui-system.md](14-frontend-ui-system.md))
- `[FEAT]` Slack DM Bot — Events API / Block Kit interactive / `/tasks` slash command. 태스크 목록 조회 및 완료 처리 DM 인터페이스 (→ [05-channels.md](05-channels.md))
- `[FEAT]` Slack 태스크 메타데이터에 assignee 정보 포함 (→ [05-channels.md](05-channels.md))
- `[FEAT]` self-DM reported-speech 메모에서 external requester 보존 (→ [08-services-business-logic.md](08-services-business-logic.md))
- `[FEAT]` IdentityResolve 토큰 사용량 로깅 (→ [15-observability.md](15-observability.md))

**Performance**
- `[PERF]` `vector_distance_cos`로 코사인 계산을 libsql 서버로 이전 — WAN 전송량 ~700× 감소 (→ [04-data-layer.md](04-data-layer.md))
- `[PERF]` `message_embeddings.vec` BLOB → F32_BLOB(768), schema v3→v4 (→ [04-data-layer.md](04-data-layer.md))
- `[PERF]` Docker COPY 레이어 순서 최적화, runtime assets final stage 분리 — go build 캐시 보존

**Refactors**
- `[REFACTOR]` `ai/` → `ai/`(Gemini SDK 결합) + `ai/core/`(순수 로직) 분리 (→ [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md))
- `[REFACTOR]` Gemini SDK `google.golang.org/genai` (v1) 마이그레이션
- `[REFACTOR]` `applyHotReload` dispatch table 전환, `HandleBackfillRoomActor` build/apply 단계 분리 (→ [11-handlers-and-api.md](11-handlers-and-api.md))
- `[REFACTOR]` legacy data migrations 제거 (production Turso 적용 검증 완료) (→ [04-data-layer.md](04-data-layer.md))
- `[REFACTOR]` safego gaps 보완, WhaTap TX 이름 통일 (→ [15-observability.md](15-observability.md))
- `[REFACTOR]` `EnrichedMessage.SenderID` → `ids.UserID` phantom type 승격

**Fixes & Quality**
- `[FIX]` gemini-embedding-001 `OutputDimensionality=768` 유지 — 모델 변경 시 차원 불일치 방지
- `[FIX]` Slack DM 봇 rate limit·retry·channel leak 방어 강화
- `[FIX]` assignee shared-fallback regression — envelope 및 room-actor fallback 경로 수정
- `[SYS]` `handleAPIError` 39 호출 사이트 표준화 로깅
- `[SYS]` `-trimpath` 빌드 플래그 추가 (deterministic output)
- `[SYS]` golangci-lint MEDIUM/LOW 지적 해소, gofmt 전체 적용
- `[TEST]` 커버리지 36.9% → 40.0% (store/handlers/services/auth/logger 추가), VCR dump gitignore, regression 통합

---

### v2.4.6 — 2026-04-19 15:33 UTC
**Commit**: `dc7c234` (근사값, 태그 미설정)

#### 사용자 관점
릴리즈 노트 내용 없음 (내부 핫픽스 또는 infra 배포로 추정).

#### 기술 관점
릴리즈 노트 내용 없음.

> Why: v2.4.6 릴리즈 노트 4종 모두 본문이 비어 있음. 태그만 생성된 내부 배포로 보인다.

---

### v2.4.5 — 2026-04-17 06:32 UTC
**Commit 범위**: `4e4a61c` 포함 구간

#### 사용자 관점 (KO)
- **내부 구조 강화**: 테스트 아키텍처 개편으로 앱 신뢰도 향상
- **최적화된 데이터 처리**: DB 초기화 방식 개선으로 빠른 응답
- **안정적인 데이터 동기화**: 특정 메시지 처리 중 데이터 오류 수정
- **깔끔해진 메시지함**: 중복 제거·보관 로직 개선

#### User Perspective (EN)
- Under-the-Hood Refinement: improved internal testing architecture
- Database Efficiency: optimized core synchronization
- Error-Free Sync: resolved message-type-specific data errors
- Smarter Archiving: enhanced deduplication cleanup

#### 기술 상세
- `[STABILITY]` 파일 기반 테스트 DB → 고유 인메모리 SQLite 교체. 테스트 간 경쟁 조건 원천 차단.
- `[OPTIMIZE]` Turso/SQLite 초기화·커넥션 풀링·스키마 마이그레이션 로직 리팩토링 (→ [04-data-layer.md](04-data-layer.md))
- `[FIX]` 빈 메타데이터의 JSON 마샬링 오류 방지
- `[FIX]` 아카이브 로직 및 메시지 중복 제거 프로세스 수정 (→ [09-identity-and-dedup.md](09-identity-and-dedup.md))

---

### v2.4.4 — 2026-04-17 04:28 UTC
**Commit 범위**: `4e4a61c` 이전 구간

#### 사용자 관점 (KO)
- **정교해진 리포트**: 표준화된 보고서 헤더·개선된 데이터 추출 엔진
- **스마트한 연락처**: WhatsApp PushName으로 실제 이름 매핑
- **명확한 담당자 식별**: 별칭·소스 컨텍스트 활용
- **성능 최적화**: 메시지 처리 상태 관리·쿼리 로직 개선
- **데이터 보호**: 메시지 처리 중 정보 누락 현상 수정

#### User Perspective (EN)
- Precision Reporting: strict report headers + improved extraction
- Identity Mapping: WhatsApp PushName resolution for contacts
- Clearer Ownership: smarter assignee identification with source context
- Architectural Refinement: status tracking and archive query overhaul
- Data Safety: fixed content-loss bug in message processing

#### 기술 상세
- `[REFACTOR]` 시간 기반 필터링을 메시지 캐시·아카이브 쿼리에서 제거 (→ [04-data-layer.md](04-data-layer.md))
- `[REFACTOR]` 메시지 처리 상태 추적을 `scan_metadata` 테이블로 이관 (→ [06-scanner-pipeline.md](06-scanner-pipeline.md))
- `[FIX]` `ExtractJSONBlock` 정규식의 공백 제거 로직 수정 — 데이터 손실 방지
- `[FEAT]` 보고서 구조 표준화: 필수 헤더 강제·시각화 데이터 추출 로직 개선 (→ [08-services-business-logic.md](08-services-business-logic.md))
- `[FEAT]` WhatsApp PushName 기반 연락처 매핑 — 레거시 별칭 기능 대체 (→ [05-channels.md](05-channels.md))
- `[FIX]` 별칭 동기화 포함 백엔드 주도 작업 분류 시스템 구현
- `[FEAT]` 담당자 식별 강화·보고서 요약에 소스 컨텍스트 추가 (→ [09-identity-and-dedup.md](09-identity-and-dedup.md))

---

### v2.4.3 — 2026-04-17 02:02 UTC
**Commit**: `dc7c234` 이전 다수 커밋

#### 사용자 관점 (KO)
- **차세대 인공지능**: gemini-3-flash-preview 탑재·16,000자 리포트 용량 확장
- **똑똑한 중복 제거**: Jaro-Winkler 알고리즘 기반 중복 작업 필터링
- **생동감 넘치는 UI**: 펄스 애니메이션·정교해진 리포트 레이아웃
- **글로벌 지원**: 다국어(i18n)·별칭 기반 담당자 식별
- **강력해진 엔진**: DB 구조 통합·최적화

#### User Perspective (EN)
- Next-Gen Intelligence: gemini-3-flash-preview + 16k report capacity
- Smart Cleanup: Jaro-Winkler deduplication for unique task lists
- Dynamic Visuals: pulse animations + refined report rendering
- Global & Personal: i18n support + alias-based assignee tracking
- Turbocharged Core: consolidated DB architecture

#### 기술 상세
- `[FEAT]` AI 모델 → `gemini-3-flash-preview`; 리포트 cutoff 16,000자 (→ [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md))
- `[FEAT]` Jaro-Winkler 유사도 기반 작업 중복 제거·동시 메시지 처리 방지 (→ [09-identity-and-dedup.md](09-identity-and-dedup.md))
- `[FEAT]` 리포트 생성에 채널/상태 필터·i18n·별칭 기반 담당자 식별 추가 (→ [08-services-business-logic.md](08-services-business-logic.md))
- `[UI]` 펄스 애니메이션 도입·리포트 렌더링 로직 고도화 (→ [14-frontend-ui-system.md](14-frontend-ui-system.md))
- `[REFACTOR]` 사용자·연락처·메시지 DB 작업 → 통합 Upsert/Update 인터페이스 (→ [04-data-layer.md](04-data-layer.md))
- `[REFACTOR]` DB 에러 로깅 중앙화·캐시 조회 입력 검증 헬퍼 도입
- `[SYS]` `ArchiveOldTasks` 영향 행 수 로깅·완료 파이프라인 조기 반환 최적화

---

### v2.4.2 — 2026-04-15 02:30 UTC
**Commit**: `8190062` 이전 구간 (추정)

#### 사용자 관점 (KO)
- **철통같은 데이터 관리**: DB 연결 안정성 강화
- **더 깊어진 업무 구조**: 서브태스크·계층적 관리 지원
- **지능형 업무 흐름**: AI 기반 비동기 상태 전환
- **기다림 없는 리포트**: 비동기 리포트 생성

#### User Perspective (EN)
- Rock-Solid Data: enhanced DB stability + comprehensive error logging
- Structured Success: hierarchical tasks + subtask support
- AI-Powered Agility: async AI-driven task state transitions
- Effortless Reporting: async report generation with status polling

#### 기술 상세
- `[STABILITY]` DB 연결 안정성 강화·상세 에러 로깅 체계 구축 (→ [04-data-layer.md](04-data-layer.md))
- `[REFACTOR]` 계층적 태스크 추출 로직 개편·서브태스크 마이그레이션 처리 (→ [08-services-business-logic.md](08-services-business-logic.md))
- `[FEAT]` AI 기반 비동기 태스크 상태 전환·자동 완료 프로세스 도입 (→ [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md))
- `[FEAT]` 상태 폴링 방식의 비동기 리포트 생성 엔진 (→ [08-services-business-logic.md](08-services-business-logic.md))

---

### v2.4.1 — 2026-04-14 05:27 UTC
**Commit**: `d5bca64` 이전 구간 (추정)

#### 사용자 관점 (KO)
- **본질에 집중**: 게임화 요소 제거·메시지 관리 본연 기능 집중
- **지능형 노이즈 필터링**: Gmail 마케팅 메일 AI 자동 감지
- **강력한 데이터 동기화**: 작업 병합·실시간 룸 잠금으로 충돌 방지
- **일관된 플랫폼 경험**: Slack·WhatsApp 처리 방식 표준화
- **투명한 필터링 통계**: AI 차단 노이즈 수치 사용량 통계 노출
- **더욱 견고해진 엔진**: 에러 처리·백그라운드 작업 전면 개선

#### User Perspective (EN)
- Clean & Focused: gamification removed, core messaging focus restored
- Smart Noise Filtering: AI-powered Gmail marketing/noise detection
- Reliable Syncing: task merging + per-room locking, conflict-free
- Unified Workflow: standardized Slack/WhatsApp message handling
- Filtering Insights: AI-filtered noise count visible in usage stats
- Rock-Solid Stability: error handling + background task overhaul

#### 기술 상세
- `[REFACTOR]` 게임화·업적 모듈 제거 — 핵심 로직·DB 스키마 간소화 (→ [04-data-layer.md](04-data-layer.md))
- `[FEAT]` Gmail AI 노이즈 필터링·마케팅 메일 감지 도입 (→ [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md), [05-channels.md](05-channels.md))
- `[FEAT]` 룸(Room) 단위 잠금 서비스 — 경쟁 조건 방지·가시성 개선 (→ [10-locking-and-concurrency.md](10-locking-and-concurrency.md))
- `[REFACTOR]` Slack/WhatsApp 스캐너 간 메시지 분류·담당자 정규화 표준화 (→ [06-scanner-pipeline.md](06-scanner-pipeline.md))
- `[REFACTOR]` API 에러 처리·컨텍스트 취소 로직 중앙 집중화
- `[FEAT]` 텍스트 유사도 서비스·시스템 프롬프트 개선 (→ [09-identity-and-dedup.md](09-identity-and-dedup.md))
- `[SYS]` 스캐너 파이프라인에 WaitGroup 도입 — 비동기 태스크 graceful shutdown (→ [06-scanner-pipeline.md](06-scanner-pipeline.md))
- `[PERF]` 인메모리 테스트 인프라 전환·상태 동기화 최적화
- `[FEAT]` 노이즈 필터링 차단 수치를 토큰 사용량 통계에 노출 (→ [15-observability.md](15-observability.md))

---

## 3. 최근 30일 Commit 요약 / Recent Commit Summary

> 기준: `git log --oneline -50` (2026-05-03 기준 상위 50커밋). 릴리즈 태그가 없는 커밋은 주제별로 그룹핑하여 정리한다.

### Feature (신규 기능)

| SHA | 제목 |
|---|---|
| `03e8fb2` | chore(logging): 태그·레벨·프리픽스 전체 정규화 |
| `a3a1e01` | 3-영업일 stale 규칙 표준화, Block Kit DM, 주간 다중 수신자 |
| `1be1772` | 일간 다이제스트 설정·서비스 통합 강화 |
| `6a40fb3` | Slack 연동 주간 리포트 기능 추가 |
| `dc7c234` | v2.7.0 버전 갱신 + Gmail 노이즈 게이트(마케팅 메시지 차단) + 테스트 추가 |
| `ceaf099` | `GetLatestThreadAssignee` — 스레드 내 최근 비공유 담당자 조회 함수 |
| `635614e` | 일간 다이제스트 서비스 구현·설정 연동 |
| `349d84f` | FTS5 아카이브 메시지 전문 검색 + `fts5` 테이블 마이그레이션 |
| `3c4e890` | `prompt_logs` 고아 테이블 제거 |
| `c5bb32c` | 대시보드 태스크 검색 입력 연결 |
| `967a927` | Slack DM 기반 마감 알림 워커 |
| `f54cc40` | 문서: Opus/Sonnet 위임 모델 섹션 추가 |
| `b68d73a` | 어드민 메뉴 + DB 오버레이 환경설정 UI |
| `355b4a3` | Cc-only Gmail 메시지 → Reference 탭 라우팅 |
| `3c1ef56` | AI 리포트별 토큰 비용 귀속 (`token_usage.report_id`) |
| `ac75598` | 소수 기반 스캐줄링으로 백그라운드 스캐너 부하 분산 (→ [06-scanner-pipeline.md](06-scanner-pipeline.md)) |
| `878fbeb` | 최종 사용자 매뉴얼 + Settings → Connections 탭 |
| `4cc34a3` | `CLAUDE.md` 명확성·구조 개선 |

### Fix (버그 수정)

| SHA | 제목 |
|---|---|
| `da00c4e` | 완료 처리: 담당자 자신의 스레드 답변에 결정론적 위임 |
| `51c9b5e` | WhatsApp/Telegram requester에 envelope sender 사용 |
| `6a253a3` | Gmail: 그룹 라우팅·자기 주소 벌크 메일 마케팅 헤더 강화 |
| `d846ebd` | AI: lite_filter에서 gray-mail 변형 감지 추가 |
| `5449d56` | Gmail 스레드 fallback 경로의 AI 토큰 누출 차단 |
| `1b24a46` | `DBKeepAliveInterval` 7초로 조정 — 스트림 처리 안정성 |
| `a294c6b` | DB keep-alive를 `PingContext` → `SELECT 1`로 교체 |
| `86b0cb2` | libsql maxIdle 0으로 복구 — 풀 재사용 시 "stream is closed" 재발 방지 |
| `a925eaa` | Gmail 시스템 프롬프트 v1.5.0 — 제3자 행위자 담당자 규칙 수정 |
| `a2d6043` | `handleResolve` 멱등성 — `[Resolved:]` 접두사 중복 방지 |
| `4878fbeb` | 채널 상태 케이싱 통일 — 소문자 표준 |

### Refactor (구조 개선)

| SHA | 제목 |
|---|---|
| `4e4a61c` | 메시지 생명주기 관리 리팩토링·SQL 쿼리 개선 |
| `91c7317` | 스캐너/서비스 의존성 역전·코드 명확성 향상 |
| `8190062` | Flow 2 Phase B — `RouteTaskByStatus`를 `HandleTaskState`로 통합 |
| `8f06c57` | Flow 2 Phase A — 태스크 전환 다중 구문 원자성 확보 |
| `8e2191a` | F-잔여 `any` sweep round 2 — i18n/chart/metadata 타입 명시 |
| `deeac60` | Gmail 이중 재시도 레이어 제거 |
| `53ecb4a` | 핸들러 12종 미테스트 커버리지 + Wave 3 페이지네이션 회귀 가드 |
| `e5945d0` | Wave 3 — `localStorage` `mc_*` 키 추상화 |
| `84852bf` | Wave 3 강화 — 페이지네이션 cap / JIT timeout / ticker 설정 / 토큰 회계 / 프롬프트 enum |
| `d5bca64` | 레거시 아키텍처 정리 + F-잔여 `any` sweep |
| `87ea441` | Wave 2 O — 약어 케이싱 통일 (Oauth → OAuth) |

### Performance (성능)

| SHA | 제목 |
|---|---|
| `4a6ba05` | 누락된 Gemini 호출 사이트에 WhaTap `trace.Step` 추가 (→ [15-observability.md](15-observability.md)) |
| `27176a6` | 패치 경계에서 이미 처리된 Gmail ID 스킵 |
| `5449d56` | Gmail 스레드 fallback 경로 AI 토큰 절감 |
| `ca2d833` | Slack 스캔을 캐시 클라이언트 경유 — `users.list` 3회/sweep 감소 |
| `9862f8e` | `GetAllUsers` 메모이즈 — 스캐너 cold-start 약 1.8s 단축 |
| `ac75598` | 소수 기반 고정 간격 교체 — 백그라운드 부하 분산 |
| `282a66d` | 소수 풀 케이던스 구현 — 스캐너 부하 분산 기술 문서 |
| `5449d56` | FTS5 검색을 대시보드 활성 태스크까지 확장 |

### Build / Chore

| SHA | 제목 |
|---|---|
| `01a2b72` | `deploy.sh` 테스트와 이미지 빌드 병렬화 — 74초 → 39초 |
| `37e22c9` | `mc-util` 별도 타겟 분리 — 기본 `make`에서 제외 |

---

## 4. 알려진 마이그레이션 / Breaking Changes

### Migrations Phase C — 완료 (2026-05-02 기준)

5개 데이터 마이그레이션 함수가 `migrations.go`에 잔존한다.

| 함수 | 상태 |
|---|---|
| `migrateTokenUsageBreakdown` | ~~제거 완료 (v2.4.7)~~ |
| `migrateTokenUsageReportID` | ~~제거 완료 (v2.4.7)~~ |
| `migrateOriginalTextOrder` | ~~제거 완료 (v2.4.7)~~ |
| `migrateMessagesFTS` | ~~제거 완료 (v2.4.7)~~ |
| `migrateContactResolution` | ~~제거 완료 (v2.4.7)~~ |

**v2.4.7 완료**: production Turso 적용 검증 후 5개 함수 전체 제거됨 (`refactor(store): drop legacy data migrations after prod parity verified`). fresh DB는 `schema.sql` 직접 적용 경로만 존재.

### Breaking Changes 이력

| 버전 | 변경 내용 | 영향 |
|---|---|---|
| v2.4.1 | 게임화·업적 DB 테이블 제거 | 해당 컬럼 직접 조회 불가. ORM/sqlc 생성 코드로만 접근 필요 |
| v2.4.3 | DB Upsert/Update 인터페이스 통합 | 이전 개별 insert 경로 제거됨 |
| v2.4.4 | `scan_metadata` 테이블로 상태 추적 이관 | 메시지 처리 상태 쿼리 경로 변경 |
| v2.4.4 | 시간 기반 필터링 메시지 캐시 쿼리에서 제거 | 이전 시간 범위 기반 API 응답 변경 가능성 |
| v2.4.7 | `message_embeddings.vec` BLOB → F32_BLOB(768), schema v4 | 기존 BLOB 임베딩 재색인 필요 (schema 자동 마이그레이션) |
| v2.4.7 | legacy data migrations 제거 (`migrations.go` 5개 함수) | production Turso 적용 완료 후 제거. fresh DB는 `schema.sql` 직접 적용 |
| v2.4.7 | `ai/` 패키지 분리 (`ai/core/` 신설) | `ai/` 직접 import 경로 변경 필요 |

---

## 5. 향후 계획 / Roadmap

다음은 코드베이스 내 명시적 TODO/보류 결정으로부터 추출한 항목이다. 추측은 포함하지 않는다.

### 확정된 보류 항목

1. ~~**Migrations Phase C 진행**~~ — **v2.4.7 완료**. 5개 마이그레이션 함수 제거됨.

2. **Key Insights 리스크 패턴 재평가 (2026-05-10)**
   - 미완료 태스크 비율이 약 10%로, 리스크 패턴 검증 데이터 부족
   - 누적 데이터 확보 후 AI 분석 프롬프트 재조정 예정
   - 참조: [08-services-business-logic.md](08-services-business-logic.md)

3. **Notion 블록 마크다운 필터링**
   - 위치: [`ai/core/analyzers.go`](../../ai/core/analyzers.go) 103번째 줄 `TODO`
   - 목적: Notion 블록에서 불필요한 마크다운 제거로 태스크 추출 컨텍스트 정제
   - 참조: [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md)

---

## 6. Cross-References

| 주제 | 챕터 |
|---|---|
| 백엔드 아키텍처 전체 | [03-backend-architecture.md](03-backend-architecture.md) |
| 데이터 레이어 (sqlc, Turso, 마이그레이션) | [04-data-layer.md](04-data-layer.md) |
| 채널 통합 (Slack, WhatsApp, Telegram, Gmail) | [05-channels.md](05-channels.md) |
| 스캐너 파이프라인 | [06-scanner-pipeline.md](06-scanner-pipeline.md) |
| AI 필터 파이프라인 | [07-ai-filter-pipeline.md](07-ai-filter-pipeline.md) |
| 서비스/비즈니스 로직 | [08-services-business-logic.md](08-services-business-logic.md) |
| ID·중복 제거 | [09-identity-and-dedup.md](09-identity-and-dedup.md) |
| 잠금·동시성 | [10-locking-and-concurrency.md](10-locking-and-concurrency.md) |
| 핸들러·API | [11-handlers-and-api.md](11-handlers-and-api.md) |
| 프론트엔드 UI 시스템 | [14-frontend-ui-system.md](14-frontend-ui-system.md) |
| 관찰 가능성 (WhaTap APM) | [15-observability.md](15-observability.md) |
