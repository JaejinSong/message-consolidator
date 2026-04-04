# Message Consolidator - TODO & TECH DEBT

## 1. 진행 예정 사항 (Planned)
- [ ] **[Infra]** 프론트엔드 컨테이너화 및 리버스 프록시 (Nginx/Caddy 도입)
- [ ] **[Infra]** Docker Compose 기반 멀티 컨테이너 오케스트레이션
- [ ] **[Auth]** JWT 기반 인증 확장 및 세션 관리 개선
- [ ] **[UI/UX]** 태스크 필터링 UI 고도화 (날짜 범위, 채널별 필터)

## 2. 기술 부채 (Tech Debt) & 개선 필요 사항
- [ ] **[Optimization]** Font Awesome 패키지 전환 (`@fortawesome/fontawesome-svg-core` 기반 트리쉐이킹 적용)
- [ ] **[Refactor]** 백엔드 에러 핸들링 통일 (커스텀 에러 타입 도입)
- [ ] **[Test]** 프론트엔드 컴포넌트 단위 테스트 (Vitest 도입 검토)

## 3. 완료 사항 (Completed)
- [x] **[Refactor]** Regex to Metadata Migration & Gmail DOM Pruning (v2.4.18)
  - WhatsApp: `waMentionRegex` 제거 및 `MentionedIDs` 기반 고정밀 멘션 인식
  - Gmail: `reQuoteStart` 정규식 제거 및 `html.Node` 트레이싱을 통한 쿼트 프루닝 적용
  - AI 프롬프트 메타데이터 구체화로 멘션/인용문 오차율 획기적 감소
- [x] **[Feature]** Context-Aware Unified AI Task Lifecycle (v2.4.17)
  - AI가 대화 맥락을 파악하여 신규 생성/업데이트/완료를 스스로 결정하는 통합 핸들러 구현
  - 7단계 대화 회귀 테스트 및 인삿말 필터링(`none` state) 적용
- [x] **[Feature]** 보관함 '병합한 업무(Merged Tasks)' 탭 및 필터링 시스템 (v2.4.16)
  - 보관함 내 전용 탭 추가 및 `status=merged` 백엔드 필터링 구현
- [x] **[Feature]** AI 인퍼런스 비동기 로깅 시스템 (DB + File) (v2.4.15)
  - Gemini 원본 응답 및 컨텍스트 비동기 저장 레이어 구축
- [x] **[Feature]** System-wide Idempotency & AI Pipeline Convergence (v2.4.13/14)
  - SQL Upsert 도입 및 단일 JSON 기반 AI 요청 파이프라인(`ai/executor.go`) 구축
- [x] **[Feature]** Task Affinity Consolidation Engine (v2.4.12)
  - 연관 업무(Affinity Score >= 80) 자동 통합 엔진 도입
- [x] **[Refactor]** 프론트엔드 TypeScript 전면 전환 및 Clean Architecture 적용 (v2.4.8)
- [x] **[Feature]** Metadata JSON 아키텍처 및 Policy 필드 도입 (v2.4.8)
- [x] **[Feature]** Gmail/Slack/WhatsApp 다중 채널 업무 추출 정밀도 고도화 (v2.4.7)

---

# AI Extraction Improvement Pipeline (Data-Driven)

## Phase 1: Data Accumulation & Observability
- **Action:** DB 원본 메시지 보존, AI 인퍼런스 로그(`ai_inference.log`) 적재, WhaTap 모니터링.

## Phase 2: Failure Case Analysis
- **Action:** 주기적 로그 분석을 통한 오탐지 케이스 식별 및 회귀 테스트 데이터셋 확충.

## Phase 3: Prompt & Logic Refinement
- **Action:** `ai/prompts/` 업데이트, 파싱 로직 보완, 화자 인식 전처리 로직 강화.

## Phase 4: Regression Testing & Deployment
- **Action:** `tests/regression/ai_regression_test.go` 검증 후 Cloud Run / VPS 배포.