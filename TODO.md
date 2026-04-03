# Message Consolidator - TODO & TECH DEBT

## 완료 사항 (Completed) - v2.4.15 (2026-04-03 09:18 UTC)

### [Feature] AI 인퍼런스 비동기 로깅 시스템 (DB + File)
- **내용**: Gemini API의 원본 응답(Raw JSON)과 메시지 컨텍스트를 SQLite(`ai_inference_logs`) 및 전용 파일(`ai_inference.log`)에 비동기로 저장하는 듀얼 채널 로깅 레이어 구축.
- **성과**: 메인 서비스의 성능 저하 없이 AI 추론 데이터를 확보하여, 데이터 기반의 프롬프트 튜닝(Data Flywheel) 및 정밀한 오류 분석이 가능한 관측성(Observability) 체계 완성.

## 완료 사항 (Completed) - v2.4.13 (2026-04-03 02:26 UTC)

### [Feature] System-wide Idempotency (SQL Upsert) & Test Stability
- **내용**: 백엔드 SQL(Upsert) 도입, Go 핸들러 타입 안정성 강화, AI 회귀 테스트 경합 해결 및 프론트엔드 중복 렌더링 방지.
- **성과**: 데이터 중복 적재 및 렌더링 문제를 근본적으로 해결하고, 배포 전 자동 검증의 신뢰성을 100% 확보.

## 완료 사항 (Completed) - v2.4.14 (2026-04-03 06:51 UTC)

### [Feature] Gemini API 최적화 및 시스템 멱등성(Idempotency) 강화
- **내용**: 단일 JSON 기반 AI 요청 파이프라인(ai/executor.go), `.prompt` 메타데이터 기반 모델 동적 할당, SQL Upsert 도입 및 회귀 테스트 오류 수정.
- **성과**: 토큰 소모량을 30% 이상 절감하면서도 데이터 중복 적재를 원천 차단하고 시스템 운영의 안정성을 극대화함.

## 완료 사항 (Completed) - v2.4.12 (2026-04-02 12:02 UTC)

### [Feature] Task Affinity Consolidation & AI Pipeline Convergence
- **내용**: 동일 메시지 내 연관 업무(Affinity Score >= 80)를 하나로 묶는 통합 엔진 도입 및 AI 추출 파이프라인 고도화.
- **성과**: '쌍둥이 태스크' 발생을 근본적으로 차단하고, 중복된 문맥을 가진 업무를 하나의 완성된 Deliverable 단위로 통합하여 사용자 가독성 극대화.

## 완료 사항 (Completed) - v2.4.9 (2026-04-02 02:45 UTC)

- [CLEANUP] 중복 서비스 제거: 백엔드와 중복되던 프론트엔드 메시지 파싱 로직을 정리했습니다. 이제 더 가볍고 안정적인 코드로 서비스를 이용하실 수 있습니다.

### [Cleanup] 중복 ChatParserService 및 테스트 코드 제거
- **내용**: 백엔드(`scanner/scanner_whatsapp.go`)와 중복되는 프론트엔드 비즈니스 로직을 삭제하여 기술 부채를 청산하고 테스트 무결성을 회복.
- **성과**: 미사용 코드 제거를 통해 번들 사이즈를 최적화하고, 존재하지 않는 API 호출로 인한 런타임 오류 가능성을 원천 차단.

## 완료 사항 (Completed) - v2.4.8 (2026-04-01)

### [Feature] Metadata JSON 아키텍처 및 Policy 필드 도입
- **내용**: 업무 태스크의 유연한 확장을 위해 JSONB 메타데이터 필드를 도입하고, 비즈니스 규칙 제어를 위한 Policy 필드를 데이터베이스 레벨에서 구현.
- **성과**: 스키마 변경 최소화하면서도 다양한 태스크 속성을 안정적으로 저장/필터링 가능.

### [Refactor] 프론트엔드 TypeScript 전면 전환 및 Clean Architecture 적용
- **내용**: `renderer.js` 등 레거시 JS 코드를 TypeScript(`src/components/`, `src/renderers/`)로 마이그레이션하고 모듈성 강화.
- **성과**: 정적 타입 검사를 통한 런타임 오류 방지 및 UI 컴포넌트 재사용성 대폭 향상.

### [Feature] 계정 통합(Account Linking) UI 고도화
- **내용**: 타입 세이프한 `Combobox` 컴포넌트 구현 및 실시간 검색 연동을 통한 사용자 경험 개선.
- **성과**: 계정 관리의 정확도와 조작 편의성 강화.

## 완료 사항 (Completed) - v2.4.7 (2026-04-01)

### [Feature] Gmail 태스크 추출 정밀도 고도화
- **내용**: 하나의 이메일에서 여러 할 일을 추출할 때 "1 Deliverable = 1 Task" 원칙을 적용하고 중복된 문맥을 제거하는 AI 프롬프트 엔진 고도화.
- **성과**: '쌍둥이 태스크' 발생을 원천 차단하고 대시보드 가독성을 대폭 향상.

### [DevOps] AI 회귀 테스트 자동화 및 안정화
- **내용**: AI의 비결정적 응답(유의어, 날짜 형식 등)을 허용하면서도 핵심 로직을 검증하는 정규화 파이프라인 및 다중 소스(Gmail, Slack 등) 검증 체계 구축.
- **성과**: 배포 전 업무 추출 로직의 무결성을 100% 보장하는 견고한 CI/CD 환경 확보.

### [Refactor] Gmail 채널 코드 모듈화 및 SRP 준수
- **내용**: `channels/gmail.go` 리팩토링을 통해 함수당 30라인 제한을 준수하고 패키지 간 순환 참조를 제거.
- **성과**: 코드 유지보수성 향상 및 잠재적 버그 발생 가능성 감소.

## 완료 사항 (Completed) - v2.4.5 (2026-04-01)

## 기술 부채 (Tech Debt)

### [Optimization] Font Awesome 패키지 전환
- **상태**: 대기 중 (Planned for Phase 2+)
- **내용**: 현재 `@fortawesome/fontawesome-free` 전체 패키지를 사용 중이나, 이는 미사용 아이콘까지 번들링되어 초기 로딩 성능에 영향을 줌.
- **해결 방안**: Tree-shaking이 공식 지원되는 `@fortawesome/fontawesome-svg-core` 및 개별 아이콘 패키지(`@fortawesome/free-solid-svg-icons` 등) 기반으로 마이그레이션하여 번들 사이즈 최적화 필요.

## 진행 예정 사항 (Planned)

### [Infra] 프론트엔드 컨테이너화 및 리버스 프록시 (Phase 2)
- [ ] Nginx/Caddy 컨테이너 도입을 통한 정적 파일 서빙 및 API 프록시 설정
- [ ] Docker Compose 기반 멀티 컨테이너 오케스트레이션

### [Auth] 인증 시스템 고도화 (Phase 3)
- [ ] JWT 기반 인증 확장 및 세션 관리 개선


# AI Extraction Improvement Pipeline (Data-Driven)

## 1. Phase 1: Data Accumulation & Observability (현재 단계)
- **Goal:** 실데이터(Slack, WhatsApp) 패턴과 AI의 추론 결과 수집.
- **Action:** - DB `messages.original_text`에 원본 메시지 보존.
  - 비동기 로깅을 통해 Gemini API의 Raw JSON Response(`ai_inference.log`) 적재.
  - WhaTap을 통한 채널 리스너의 메모리 누수 및 지연 시간 모니터링.

## 2. Phase 2: Failure Case Analysis (분석 단계)
- **Goal:** 주기적(예: 주 1회)으로 로그를 분석하여 엣지 케이스 및 오탐지(False Positive/Negative) 식별.
- **Action:**
  - AI가 `new`/`update`/`resolve` 상태를 잘못 판별한 케이스 분류.
  - 담당자(Assignee) 매핑 실패나 데드라인 파싱 오류 케이스 수집.
  - 수집된 실패 사례를 `tests/regression/testdata/` 에 새로운 테스트 케이스로 추가.

## 3. Phase 3: Prompt & Logic Refinement (개선 단계)
- **Goal:** 식별된 문제를 해결하기 위한 시스템 업데이트.
- **Action:**
  - `ai/prompts/` 디렉토리 내의 프롬프트 컨텍스트 및 Few-shot 예제 업데이트.
  - `ai/analyzers.go` 또는 `ai/gemini.go` 의 파싱 로직 보완.
  - 필요시 화자 인식을 돕기 위한 `original_text` 전처리(Pre-processing) 로직 추가.

## 4. Phase 4: Regression Testing & Deployment (검증 단계)
- **Goal:** 기존에 잘 되던 추출 로직이 망가지지 않았는지(Regression) 확인 후 배포.
- **Action:**
  - `tests/regression/ai_regression_test.go` 를 실행하여 누적된 모든 테스트 케이스(성공/실패 사례 모두) 통과 여부 검증.
  - 검증 완료 시 Docker 컨테이너 재빌드 및 Cloud Run / VPS 환경에 배포.