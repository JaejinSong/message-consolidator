# Message Consolidator - TODO & TECH DEBT

## 1. 진행 예정 사항 (Planned)
- [ ] **[Auth]** JWT 기반 인증 확장 및 세션 관리 개선
- [ ] **[UI/UX]** 태스크 필터링 UI 고도화 (날짜 범위, 채널별 필터)

## 2. 기술 부채 (Tech Debt) & 개선 필요 사항
- [ ] **[Optimization]** Font Awesome 패키지 전환 (`@fortawesome/fontawesome-svg-core` 기반 트리쉐이킹 적용)
- [ ] **[Refactor]** 백엔드 에러 핸들링 통일 (커스텀 에러 타입 도입)
- [ ] **[WhatsApp]** 시스템 메시지 필터링 정교화 (현재 `StubType`, `ProtocolMessage` 광범위 차단 중. 향후 "그룹 이름 변경"이나 "참여자 초대/강퇴" 알림 활용 필요 시 필터링 완화 검토)

## 3. 완료 사항 (Completed)
- [x] **[Infra]** Docker Compose & Caddy 기반 VPS 배포 자동화 (v2.4.19)
  - `scripts/` 내 중복 `main` 패키지 충돌 해결 (cmd/verify 하위로 리팩토링)
  - Artifact Registry 및 GCS 연동 배포 워크플로우 구축
  - Caddy 리버스 프록시 및 SSL 설정 완료
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
