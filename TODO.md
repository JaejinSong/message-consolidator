# Message Consolidator - TODO & TECH DEBT

## 1. 진행 예정 사항 (Planned)
- [ ] **[Auth]** JWT 기반 인증 확장 및 세션 관리 개선
- [ ] **[UI/UX]** 태스크 필터링 UI 고도화 (날짜 범위, 채널별 필터)

## 2. 기술 부채 (Tech Debt) & 개선 필요 사항
- [ ] **[Refactor]** 백엔드 에러 핸들링 통일 (커스텀 에러 타입 도입)
- [ ] **[WhatsApp]** 시스템 메시지 필터링 정교화 (현재 `StubType`, `ProtocolMessage` 광범위 차단 중. 향후 "그룹 이름 변경"이나 "참여자 초대/강퇴" 알림 활용 필요 시 필터링 완화 검토)

## 3. 완료 사항 (Completed)
- [x] **[Feature]** 태스크 수동 병합(Manual Merge) 및 시맨틱 통합 로직 (v2.4.21)
  - 백엔드 병합 API 구현 및 타임스탬프 기반 이력 관리
  - 프론트엔드 병합 UI 및 드래그 앤 드롭 인터랙션 고도화
- [x] **[Insights]** 리포트 자동 로드 및 데이터 동기화 최적화 (v2.4.21)
  - 대규모 데이터셋(10k+) 대응 로딩 가속화 및 startup view migration
- [x] **[Infra]** Docker 빌드 레이어 최적화 및 폰트 서브셋(WOFF2) 적용 (v2.4.21)
  - 폰트 어썸 의존성 제거로 이미지 크기 75% 절감 (1.2GB -> 280MB)
- [x] **[I18N]** 기본 언어 영어(EN) 전환 및 지능형 캐시 무효화 (v2.4.21)
  - 모든 탭(KO/ID/EN) 간 실시간 정합성 보장 로직 도입
- [x] **[UI/UX]** Optimistic UI 도입 및 테스크 처리 지연(0ms) 해결 (v2.4.20)
  - 삭제 애니메이션 적용 및 개별 DOM 조작(`renderer.ts`) 리팩토링
  - API 실패 시 상태/UI 롤백 로직 도입
- [x] **[Infra]** Docker 빌드 파일 누락 (`index.html`, `whatap.conf` 등) 수정 (v2.4.20)
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
