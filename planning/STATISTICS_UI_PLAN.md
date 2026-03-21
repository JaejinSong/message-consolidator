# [Plan] Anki-style Work Statistics & Productivity Insights

## 1. 개요
Anki의 학습 통계 시스템을 벤치마킹하여 `message-consolidator`에 업무 지표 시각화 및 생산성 인사이트 기능을 도입합니다. 사용자의 업무 몰입도를 높이고 AI 활용 효율성을 시각적으로 체감할 수 있도록 구성합니다.

## 2. 주요 구성 요소

### A. UI/UX 레이아웃
- **Insights 전용 탭**: 메인 네비게이션 (`Dashboard | Insights | Archive`)
- **Daily Summary Bar**: 대시보드 상단에 위치한 실시간 한 줄 요약 지표
- **Visualization**: `Chart.js`를 이용한 프리미엄 차트 (히트맵, 피크 타임 등)

### B. 주요 통계 지표
- **생산성 히트맵**: GitHub 스타일의 일일 업무 완료 달성도
- **업무 처리 속도**: 추출부터 완료까지의 리드 타임 분석
- **AI 도움 지표**: AI 요약을 통해 절약된 예상 시간 및 토큰 대비 업무 추출 효율
- **피크 타임**: 가장 업무 효율이 높은 시간대 분석

## 3. 구현 단계 (Proposed Changes)

### Phase 1: 기반 인프라 구축
- [ ] Go 백엔드: `/api/user/stats` 엔드포인트 및 데이터 집계 로직 구현
- [ ] DB: 통계 쿼리 최적화를 위한 인덱스 추가

### Phase 2: 프론트엔드 네비게이션 및 레이아웃
- [ ] `index.html`: Insights 탭 및 섹션 구조 추가
- [ ] `app.js`: 뷰 전환 로직 및 데이터 페칭 구현
- [ ] `style.css`: 통계 전용 유리 질감(Glassmorphism) 카드 스타일링

### Phase 3: 시각화 및 고도화
- [ ] `Chart.js` 연동 및 주요 차트 렌더링 로직 구현
- [ ] AI 기반 'Daily Glance' 인사이트 문구 생성 로직 추가

## 4. 검증 계획
- [ ] 다크/라이트 테마 시독성 및 모바일 반응형 체크
- [ ] 업무 완료 시 실시간 가디언(XP/Points)과 통계 데이터 일관성 확인
