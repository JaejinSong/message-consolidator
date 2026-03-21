# [Plan] Anki-style Work Statistics & Productivity Insights

## 1. 개요
Anki의 학습 통계 시스템을 벤치마킹하여 `message-consolidator`에 업무 지표 시각화 및 생산성 인사이트 기능을 도입합니다. 사용자의 업무 몰입도를 높이고 AI 활용 효율성을 시각적으로 체감할 수 있도록 구성합니다.

## 2. 주요 구성 요소

### A. UI/UX 레이아웃
- **Insights 전용 탭**: 메인 네비게이션 (`Dashboard | Insights | Archive`)
- **Daily Summary Bar**: 대시보드 상단에 위치한 실시간 한 줄 요약 지표
- **Visualization**: 성능 최적화 및 테마 호환성을 위한 순수 CSS/SVG 기반의 무의존성(Zero-dependency) 경량 시각화 (히트맵, 수평 스택 바 등)

### B. 주요 통계 지표
- **생산성 히트맵**: GitHub 스타일의 일일 업무 완료 달성도
- **피크 타임**: 가장 업무 효율이 높은 시간대 분석
- **채널별 업무 유입 비중 (Source Distribution)**: Slack, WhatsApp, Gmail 중 어느 채널에서 업무가 집중되는지 파이 차트로 시각화 (앱의 핵심 가치 증명)
- **방치된 업무 알림 (Actionable Insight)**: 생성된 지 3일 이상 경과했으나 완료되지 않은 업무 비율 및 알림
- **AI 효용성 지표**: 절약된 예상 시간 (단순화된 고정 공식 활용)

## 3. 구현 단계 (Proposed Changes)

### Phase 1: 기반 인프라 구축
- [ ] Go 백엔드: `/api/user/stats` 엔드포인트 구축 (퍼포먼스를 위해 기존 `messageCache` 및 `archiveCache`를 활용한 인메모리 집계 우선)
- [ ] 백엔드 성능 보호: 집계된 통계 결과의 단기 캐싱(예: 1시간) 레이어 추가
- [ ] DB: 통계 쿼리 최적화를 위한 인덱스 추가

### Phase 2: 프론트엔드 네비게이션 및 레이아웃
- [ ] `index.html`: Insights 탭 및 섹션 구조 추가
- [ ] `app.js`: 뷰 전환 로직 및 데이터 페칭 구현
- [ ] `style.css`: 통계 전용 유리 질감(Glassmorphism) 카드 스타일링

### Phase 3: 시각화 및 고도화
- [ ] CSS Grid 기반 히트맵 및 Flexbox 기반 수평 스택 바 렌더링 컴포넌트 구현
- [ ] 통계 기반 'Daily Glance' 텍스트 동적 조합 (비용/속도를 위해 AI 대신 Rule-based 텍스트 포매팅 사용)

## 4. 검증 계획
- [ ] 다크/라이트 테마 시독성 및 모바일 반응형 체크
- [ ] 업무 완료 시 실시간 가디언(XP/Points)과 통계 데이터 일관성 확인
