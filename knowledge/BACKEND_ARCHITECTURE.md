# Backend Architecture & Maintenance Guide

본 문서는 `message-consolidator` 백엔드(Go) 프로젝트의 구조, 설계 철학, 그리고 패키지별 역할을 정의한 유지보수 가이드입니다.

## 1. 설계 철학 (Design Principles)
* **도메인 주도 패키지 분리**: 루트 디렉터리에 집중된 코드를 배제하고, `handlers`, `services`, `store`, `channels` 등 철저한 패키지 기반 모듈화를 지향합니다.
* **무중단 및 안정성 (Resilience)**: 외부 API(Slack, WhatsApp 등)의 Rate Limit(429) 대응, 네트워크 지연, 예기치 않은 패닉에 대비한 방어 로직과 자동 재시도 기능을 내장합니다.
* **고성능 데이터 파이프라인**: 대량의 메시지를 처리하기 위해 배치 처리(Batching), 청킹(Chunking), 인메모리 캐싱 및 지연 로딩(Lazy Loading)을 적극 활용합니다.
* **클라우드 네이티브 (Cloud-Native)**: 스탠드얼론 서버 모드와 서버리스(Google Cloud Run) 모드를 환경 변수 하나로 전환할 수 있는 유연한 아키텍처를 가집니다.

---

## 2. 디렉터리 및 패키지 구조

### 🚀 Entry Point & Config
| 패키지/파일명 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`main.go`** | **Entry Point** | 앱 초기화, 라우팅 등록, 백그라운드 스캐너 기동, 그리고 `SIGINT/SIGTERM` 시그널에 따른 **Graceful Shutdown**을 관장합니다. |
| **`config`** | **Configuration** | `.env` 및 환경 변수(Turso DB, API Keys 등)를 구조체로 파싱하고 전역으로 제공합니다. |
| **`logger`** | **Logging/APM** | 서비스 전반의 로그 규격화 및 WhaTap Go Agent 기반의 분산 추적(APM) 래퍼를 제공합니다. |

### 🧠 Core Domain
| 패키지 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`handlers`** | **HTTP Transport Layer** | 클라이언트의 요청(Request)을 받아 검증하고, `services`를 호출한 뒤 HTTP 표준(JSON)으로 응답을 반환합니다. |
| **`services`** | **Business Logic Layer** | 업무 재분류, 게이미피케이션(XP, 스트릭, 업적), 에일리어스 정규화 등 핵심 비즈니스 로직을 수행합니다. 핸들러와 DB를 느슨하게 결합합니다. |
| **`store`** | **Data Access Layer** | 데이터베이스(Turso/libsql)와의 통신을 전담합니다. SQL `VIEW` 활용, 커넥션 풀링 최적화, 그리고 인메모리 캐시 동기화를 담당합니다. |
| **`types`** | **Shared Types** | 패키지 간 순환 참조(Circular Dependency)를 방지하기 위해 공통으로 사용되는 데이터 구조체(Struct)를 정의합니다. |

### 🔌 Integrations & Engines
| 패키지 | 역할 | 상세 설명 |
| :--- | :--- | :--- |
| **`ai`** | **AI Engine** | Google Gemini API 연동 모듈입니다. 토큰 비용을 최소화하기 위한 프롬프트 경량화, 결과 파싱, 번역 작업 등을 수행합니다. |
| **`channels`** | **Platform Adapters** | 외부 SaaS와의 통신을 담당합니다.<br>- `Slack`: API 페이지네이션 및 Rate Limit 핸들링<br>- `WhatsApp`: `whatsmeow` 라이브러리 기반 세션 관리<br>- `Gmail`: Thread 최신 메시지 추출 및 인증 처리 |
| **`scanner`** | **Background Worker** | 등록된 채널들을 주기적으로 폴링(Polling)하여 신규 메시지를 수집하고 AI 분석 파이프라인으로 전달하는 비동기 워커입니다. |
| **`auth`** | **Authentication** | Google OAuth 로그인 흐름, 세션 쿠키 발급 및 보안(Security) 관련 미들웨어를 제공합니다. |

---

## 3. 핵심 아키텍처 흐름 (Data Flow)

### 플로우 A: 백그라운드 메시지 수집 및 분석 (Scanner Flow)
1. **Trigger**: `scanner` 패키지가 정해진 주기(또는 Cloud Scheduler)에 따라 각 `channels` 스캔 명령 호출.
2. **Fetch**: `channels`가 Slack, WA, Gmail에서 원시 메시지를 수집 (페이지네이션 적용).
3. **Analyze**: 수집된 메시지 묶음을 `ai` 패키지(Gemini)로 전달하여 업무(Task), 요청자(Requester) 등을 추출 (Batch Chunking 적용).
4. **Normalize**: `services` 레이어에서 사용자가 등록한 규칙(Contact Mapping 등)에 따라 이름 등을 정규화.
5. **Store**: `store` 패키지를 통해 Turso DB에 배치 저장 후 인메모리 캐시(`RefreshCache`) 갱신.

### 플로우 B: 사용자 API 요청 (User Request Flow)
1. **Request**: 사용자가 `/api/messages/done` 호출 (업무 완료 처리).
2. **Middleware**: `auth` 미들웨어가 쿠키를 검증하여 세션 통과.
3. **Handler**: `handlers`가 JSON 파싱 후 `services.ToggleTaskDone` 호출.
4. **Service**: DB 업데이트와 더불어, **Gamification 로직**을 실행 (XP 지급, 스트릭 업데이트, 업적 달성 검사).
5. **Store**: Turso DB 트랜잭션 커밋 및 해당 사용자의 통계 캐시 무효화.
6. **Response**: 갱신된 최신 통계(`UserStats`)와 함께 성공 응답 반환.

---

## 4. 핵심 기술 및 안정성 전략 (Resilience)

* **Turso (libsql) DB 마이그레이션**:
  * 엣지 분산 처리에 특화된 SQLite 기반의 Turso를 메인 DB로 사용합니다. 간헐적 Stream Closed 이슈를 막기 위한 맞춤형 Connection Pooling이 적용되어 있습니다.
* **인메모리 데이터 캐싱 (Data Caching)**:
  * 무거운 집계 쿼리를 방지하기 위해 사용자별 업무 리스트와 대시보드 통계는 메모리에 캐싱됩니다. 데이터 변경 작업(Save, Delete, Update) 발생 시 즉시 캐시를 갱신합니다.
* **Graceful Shutdown**:
  * 배포나 서버 종료(`SIGINT`/`SIGTERM`) 시, 진행 중인 데이터베이스 트랜잭션을 안전하게 마무리하고, 메모리 캐시에 남아 있는 토큰 사용량/메타데이터를 DB로 즉시 플러시(Flush)한 후 앱을 종료합니다.
* **동적 런타임 최적화**:
  * 환경 변수(`CLOUD_RUN_MODE=true`)에 따라 백그라운드 루프를 끄고 API(Webhook) 방식의 1회성 스캔 모드로 동작할 수 있어 서버리스 과금 환경에 완벽하게 대응합니다.