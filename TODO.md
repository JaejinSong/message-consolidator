# Message Consolidator - TODO & TECH DEBT

## 완료 사항 (Completed) - v2.4.5 (2026-04-01)

### [Feature] WhatsApp Meow API 통합 및 태스크 자동 할당
- **내용**: WhatsApp 메시지 수신 시 기존 작업을 조회하여 자동 연결하거나 신규 작업을 생성하는 DB-First 로직 구현.
- **성과**: 중복 태스크 생성을 방지하고 대화 문맥에 따른 업무 관리가 가능해짐.

### [Monitoring] WhaTapGo 모니터링 에이전트 도입
- **내용**: 백엔드 서비스에 WhaTap 에이전트를 내장하여 실시간 리소스 및 성능 모니터링 체계 구축.
- **성과**: 장애 대응력 향상 및 시스템 가용성 확보.

### [Infra] React SPA 전환 기초 작업
- **내용**: Vite 기반 프론트엔드 빌드 환경 구축 및 TypeScript 전환 기초 단계 완료.

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
