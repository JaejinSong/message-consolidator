# Message Consolidator - TODO & TECH DEBT

## 완료 사항 (Completed) - v2.4.13 (2026-04-03 02:26 UTC)

### [Feature] System-wide Idempotency (SQL Upsert) & Test Stability
- **내용**: 백엔드 SQL(Upsert) 도입, Go 핸들러 타입 안정성 강화, AI 회귀 테스트 경합 해결 및 프론트엔드 중복 렌더링 방지.
- **성과**: 데이터 중복 적재 및 렌더링 문제를 근본적으로 해결하고, 배포 전 자동 검증의 신뢰성을 100% 확보.

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
