# 99 — Glossary / 용어 사전

> 알파벳/가나다 혼합 정렬. 코드를 읽다 모르는 단어가 나오면 여기서 즉시 찾으세요.
>
> 형식: **용어 (KO / EN)** — 한 줄 정의 — 코드 위치 (있을 때)

---

## A

**Activity Customer (활동 고객)**
— AI가 메시지 스레드에서 추론한 "실제 업무 관련 고객". 단순 참조자나 CC 수신자와 구분하기 위해 `services` 레이어에서 별도 추론 로직을 적용합니다.
— [`services/`](../../services/)

**Affinity Group (친화 그룹)**
— AI가 부여하는 `affinity_group_id`. Jaro-Winkler 유사도가 낮더라도 맥락상 같은 업무로 판단된 태스크를 하나의 그룹으로 묶어 중복 생성을 방지합니다.
— [`ai/analyzers.go`](../../ai/analyzers.go)

**Alias (별칭)**
— 동일 인물을 가리키는 여러 이름 표현 (예: "나", "Me", "Song"). DSU로 병합되어 하나의 Contact로 정규화됩니다.
— [`store/`](../../store/)

**APM (Application Performance Monitoring)**
— 분산 추적·트랜잭션 가시성 도구. 이 프로젝트에서는 WhaTap APM을 수동 계측(manual instrumentation)으로 사용합니다.
— [`handlers/middleware_whatap.go`](../../handlers/middleware_whatap.go)

**Archive (아카이브)**
— `AUTO_ARCHIVE_DAYS` 설정값을 초과한 완료 태스크를 비활성 상태로 전환하는 동작. 삭제가 아닌 숨김.
— [`store/archive_store.go`](../../store/archive_store.go)

---

## B

**BEM (Block Element Modifier)**
— CSS 클래스 명명 규칙. `block__element--modifier` 형식. 이 프로젝트의 모든 CSS는 BEM을 따르며 `px`/`hex` 하드코딩은 금지.
— [`src/`](../../src/)

**Background Scanner (백그라운드 스캐너)**
— `scanner` 패키지가 관리하는 비동기 워커. Prime-Pool 소수 주기로 4개 채널을 독립적으로 폴링합니다.
— [`scanner/`](../../scanner/)

---

## C

**CGO_ENABLED=0**
— Go 빌드 시 C 라이브러리 링크를 비활성화하여 정적 바이너리를 생성하는 플래그. Docker 컨테이너 호환성과 UPX 압축을 위해 필수.
— [`Makefile:19`](../../Makefile#L19)

**Consolidation (통합)**
— 여러 채널(Slack·Gmail·Telegram·WhatsApp)의 메시지를 하나의 태스크 관리 뷰로 통합하는 핵심 도메인 행위.

**Contact (연락처)**
— 시스템에 등록된 사람 엔터티. `Alias`가 여러 개일 수 있으며, DSU로 병합·정규화됩니다.

**Context Window Optimization (컨텍스트 윈도우 최적화)**
— Step 1 Raw Parser가 Gmail은 15,000자, Chat은 30,000자로 입력을 잘라 LLM 토큰 낭비를 방지하는 기법.
— [TECH.md](../../TECH.md)

---

## D

**Daily Digest (일별 요약)**
— 매일 지정 시각에 지정 수신자에게 Slack Block Kit DM으로 발송되는 업무 요약 리포트.
— [`services/reports_service.go`](../../services/)

**DB (Database)**
— Turso (libsql) 기반 SQLite edge DB. `store/` 레이어를 통해서만 접근.

**DI (Dependency Injection / 의존성 주입)**
— `main.go`에서 구체 구현체를 생성하고 인터페이스로 레이어에 주입하는 패턴. Store 레이어를 교체 가능하게 유지.

**Distributed Lock (분산 락)**
— 동일 채팅방·이메일 스레드에 대해 AI 추론이 동시에 중복 실행되는 것을 막는 `sync.Map` 기반 in-flight lock.
— [`scanner/`](../../scanner/) 내 RoomLock 패턴

**DSU (Disjoint Set Union / 분리 집합)**
— Alias 병합에 사용하는 알고리즘. 여러 이름 표현을 단일 정식 Contact ID로 합치기 위해 Union-Find 구조를 활용.

---

## E

**Event-driven UI (이벤트 기반 UI)**
— 프론트엔드가 서버 폴링 대신 사용자 이벤트(클릭·입력)와 명시적 API 호출로만 상태를 갱신하는 Clean Architecture 패턴.

---

## F

**Few-shot (퓨샷)**
— 프롬프트에 예시(few examples)를 포함하여 LLM이 원하는 출력 형식을 모방하도록 유도하는 기법. 태스크 추출 프롬프트에 적용.
— [`ai/prompts/`](../../ai/prompts/)

**Flash-Lite (Gemini Flash-Lite)**
— Step 2에서 noise 이진 필터로 사용하는 초경량 Gemini 모델. 인사말·광고·OTP 등을 빠르게 제거해 비용을 최소화.

**FTS (Full-Text Search / 전문 검색)**
— SQLite의 FTS5 확장으로 메시지/태스크를 텍스트 검색하는 기능. Phase C 마이그레이션 후 적용 예정.

---

## G

**Gemini**
— Google의 멀티모달 LLM 시리즈. 이 프로젝트는 Flash-Lite(노이즈 필터), Flash(태스크 추출), Pro(리포트 생성) 세 계층으로 구분 사용.
— [`ai/gemini.go`](../../ai/gemini.go)

**Graceful Shutdown (그레이스풀 셧다운)**
— SIGINT/SIGTERM 수신 시 외부 채널 연결 해제 → 메모리 플러시 → HTTP drain → DB 종료를 병렬·순서 보장하여 데이터 유실 없이 종료.
— [`main.go:143`](../../main.go#L143)

**Guard Clause (가드 절)**
— 중첩을 줄이기 위해 오류/엣지 케이스를 함수 상단에서 early return으로 처리하는 패턴. 프로젝트 Go 규칙으로 강제.

**gotd/td**
— Telegram MTProto 프로토콜 구현 라이브러리. TL 스키마 기반으로 Telegram 클라이언트 세션을 직접 관리.
— [`go.mod`](../../go.mod)

---

## H

**Harmonic Resonance (조화 공명)**
— 두 주기 타이머가 LCM 주기로 정렬되어 동시에 발화하는 현상. Prime-Pool로 구조적 회피.
— [TECH.md § 4](../../TECH.md)

---

## I

**Identity (아이덴티티)**
— "나", "Me", `__CURRENT_USER__` 등 자기 지칭 표현을 실제 사용자 ID·이름으로 정규화하는 AI 모듈.
— [`ai/`](../../ai/) — `IdentityResolver`

**In-flight Lock (인플라이트 락)**
— 동일 리소스(채팅방 ID, 스레드 ID)에 대해 처리 중인 요청이 있을 때 중복 실행을 skip하는 동시성 보호 패턴.

---

## J

**Jaro-Winkler**
— 두 문자열의 의미적 유사도를 [0,1]로 측정하는 알고리즘. 기본 임계값 0.85 이상이면 동일 태스크로 간주해 중복 생성을 막음.
— [`ai/analyzers.go`](../../ai/analyzers.go)

**JWT (JSON Web Token)**
— 서명된 토큰 포맷. 이 프로젝트에서는 세션 쿠키 방식을 사용하며 JWT는 직접 노출되지 않음.

---

## L

**libsql**
— Turso가 fork한 SQLite 확장 라이브러리. HTTP·WebSocket 프로토콜로 원격 SQLite에 접근 가능.
— [`go.mod` — `tursodatabase/libsql-client-go`](../../go.mod)

---

## M

**MCP (Model Context Protocol)**
— AI 에이전트가 외부 도구(DB, 파일 시스템, API)에 표준화된 방법으로 접근하는 프로토콜. 개발 도구(Serena, Turso MCP 서버 등)에 활용.

**Message (메시지)**
— Slack 메시지·Gmail 스레드·Telegram 메시지·WhatsApp 메시지 등 원본 채널 데이터. 스캐너가 수집하여 AI 파이프라인에 전달.

**MTProto**
— Telegram 독자 암호화 프로토콜. `gotd/td`가 이를 구현하여 공식 Telegram 클라이언트 수준의 세션 관리 제공.

---

## N

**Noise Gate (노이즈 게이트)**
— Step 1 Raw Parser 단계에서 인사말·광고·OTP·부재중 알림 등을 LLM 호출 전에 제거하는 사전 필터.
— [`channels/`](../../channels/) gmail noise gate 등

---

## O

**OAuth (Open Authorization)**
— 구글 계정 기반 로그인·Gmail 접근 권한 위임에 사용. `auth/` 패키지가 callback 흐름을 관리.
— [`auth/`](../../auth/)

---

## P

**Phantom Type (팬텀 타입)**
— `type UserID int64` 처럼 컴파일러 수준에서 ID 타입을 구분하는 패턴. 단순 `int64`를 금지해 ID 혼용 버그를 방지.
— [CLAUDE.md](../../CLAUDE.md)

**Prime-Pool (소수 풀)**
— 8개 백그라운드 loop가 매 tick마다 소수 초(`59, 61, 67, 71, 73`)를 랜덤 추첨하여 외부 cron과의 harmonic resonance를 회피하는 분산 스케줄링 설계.
— [`scanner/scanner_loop.go`](../../scanner/)

**Pro (Gemini Pro)**
— Step 4 리포트 생성에 사용하는 고성능 Gemini 모델. 복잡한 컨텍스트 분석과 전략적 비즈니스 인사이트 도출.

---

## R

**RAG (Retrieval-Augmented Generation)**
— DB에 저장된 과거 태스크·컨텍스트를 LLM 프롬프트에 삽입하여 할루시네이션을 줄이는 기법.
— [`ai/rag.go`](../../ai/rag.go)

**Reminder (리마인더)**
— 기한 초과·스테일 태스크를 지정 시간대에 Slack Block Kit DM으로 알려주는 기능.

**Report (리포트)**
— Daily Digest(일별)와 Weekly Report(주별) 두 종류. AI Pro 모델이 축적된 태스크를 분석해 생성.
— [`services/reports_service.go`](../../services/)

---

## S

**Scanner (스캐너)**
— `scanner` 패키지. Prime-Pool 소수 주기로 4개 채널을 독립 ticker로 폴링하여 AI 파이프라인에 전달하는 비동기 워커.
— [`scanner/`](../../scanner/)

**SDK (Software Development Kit)**
— 외부 서비스 연동 라이브러리 묶음. slack-go, whatsmeow, gotd/td, google Gemini SDK 등.

**Skip-when-running (실행 중 스킵)**
— atomic CAS로 구현. 한 사이클이 다음 tick까지 늘어져도 queue 폭증 없이 단순 skip. 회복력 확보 목적.
— [TECH.md § 4](../../TECH.md)

**sqlc**
— SQL 쿼리 파일(`store/queries/*.sql`)에서 타입 안전한 Go 코드(`db/*.sql.go`)를 자동 생성하는 도구. 생성 파일 직접 수정 금지.
— [`store/queries/`](../../store/queries/)

**SSOT (Single Source of Truth / 단일 진실 공급원)**
— 코드·문서·설정에서 특정 사실의 유일한 출처를 하나로 지정하는 원칙. 충돌 시 코드가 SSOT.

**Stale (스테일)**
— 3 근무일 이상 업데이트 없는 미완료 태스크. 알림 대상 및 리포트 강조 대상.
— [`store/active_search_store.go`](../../store/active_search_store.go)

---

## T

**Task (태스크)**
— AI가 메시지에서 추출한 실행 가능한 업무 단위. TASK / POLICY / QUERY / PROMISE 카테고리로 분류.
— [`db/models.go`](../../db/models.go)

**Thread-Aware Intelligence (스레드 인식 지능)**
— 메시지의 Reply-To 관계를 추적하여 답변이 달렸을 때 태스크가 완료되었는지를 자동 판별하는 로직.

**Time-Topic Hybrid Grouping (시간-주제 혼합 그룹화)**
— 동일 발신자가 짧은 간격으로 보낸 여러 메시지를 하나의 컨텍스트로 묶어 LLM에 전달하는 Step 1 최적화.

**Turso**
— libsql 기반 edge-distributed SQLite 서비스. 간헐적 Stream Closed 이슈를 막기 위한 맞춤형 connection pooling과 `SELECT 1` keepalive 적용.
— [`store/db.go`](../../store/db.go)

**TX (Transaction / 트랜잭션)**
— WhaTap APM 컨텍스트에서 TX는 하나의 측정 단위 이름. Background TX 이름은 반드시 `/`로 시작해야 WhaTap 대시보드에 표시됨.

---

## U

**UPX**
— 실행 파일 압축 도구. `make build-backend`에서 `-1` 옵션으로 빌드 후 바이너리를 압축. 제거 제안 금지.
— [`Makefile:20`](../../Makefile#L20)

---

## V

**Vite**
— 프론트엔드 빌드 도구. 개발 시 HMR, 프로덕션 시 번들 최적화. `/api` 요청을 Go 서버로 프록시.
— [`package.json`](../../package.json)

---

## W

**Weekly Report (주간 리포트)**
— 매주 지정 요일/시각에 발송되는 Slack DM 업무 요약. Daily Digest와 동일한 ReportsService를 통해 생성.
— [`services/reports_service.go`](../../services/)

**WhaTap APM**
— WhaTap의 Go 트레이싱 에이전트. `trace.Init()` 호출 없이는 모든 계측이 silent no-op. 수동 계측만 허용(`whatap-go-inst` 금지).
— [`handlers/middleware_whatap.go`](../../handlers/middleware_whatap.go) · [`store/db.go`](../../store/db.go)

**whatsmeow**
— WhatsApp Web Protobuf 기반 Go 클라이언트 라이브러리. 세션 관리(QR 코드 → 페어링 → 유지)를 담당.
— [`go.mod`](../../go.mod)

---

## 기타 약어 / Other Abbreviations

| 약어 | 풀네임 | 컨텍스트 |
|---|---|---|
| BE | Backend | Go 서버 |
| FE | Frontend | Vite + TypeScript |
| DSN | Data Source Name | DB 연결 문자열 (Turso URL + Token) |
| HMR | Hot Module Replacement | Vite 개발 서버 기능 |
| LLM | Large Language Model | Gemini 시리즈 |
| LCM | Least Common Multiple | Prime-Pool 조화 공명 회피 근거 |
| ORM | Object-Relational Mapping | sqlc가 이 역할을 수행 |
| QR | Quick Response (code) | WhatsApp 최초 페어링 시 사용 |
| VPS | Virtual Private Server | GCP e2-micro 인스턴스 |
| XP | Experience Points | 게이미피케이션 시스템 경험치 |
