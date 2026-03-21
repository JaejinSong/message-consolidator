# TODO List

## PC 버전 업무리스트 가독성 개선 (Readability)

사용자가 업무 정보를 더 빠르고 명확하게 파악할 수 있도록 UI/UX를 개선합니다.

- [ ] **가독성 중심의 멀티 테마 지원**
  - 다크(Dark) / 라이트(Light) 테마 전환 기능 및 UI 테마 토글 버튼 추가
  - `localStorage`를 이용한 사용자 테마 설정 저장 및 복원
- [ ] **행 레이아웃 및 간격 최적화**
  - 행 간격(Gap) 및 수직 패딩 조정을 통한 시각적 여백 확보
- [ ] **텍스트 시독성 향상**
  - 업무 내용(Task) 최대 2줄 표시 (`line-clamp`) 및 폰트 크기 조정
  - 방(Room) 이름을 배지(Badge) 형태로 표시하여 구분감 강화
- [ ] **시간 표시 지능화 (Relative Time)**
  - 당일 업무에 대해 "방금 전", "n시간 전" 등 상대 시간 표시 도입
  - 과거 업무의 날짜 표기 간소화

---

## 게이미피케이션 (Gamification) - 보상 및 중독성 설계

사용자의 업무 몰입도를 높이기 위해 Duolingo, GitHub 등 성공적인 서비스의 핵심 요소를 벤치마킹하여 도입합니다.

- [ ] **DB 스키마 확장 및 마이그레이션**
  - `users` 테이블: `points`, `streak`, `level`, `xp`, `last_completed_at`, `daily_goal` 컬럼 추가
- [ ] **연속성 시스템 (Continuity & Streaks)**
  - [ ] **데일리 스트릭**: 매일 최소 1개 업무 완료 시 스트릭 유지 (Duolingo Style)
  - [ ] **스트릭 프리즈**: 포인트로 구매 가능한 스트릭 보호권
  - [ ] **생산성 히트맵**: GitHub 커밋 그래프 형태의 일일 업무 달성도 시각화
- [ ] **보상 및 피드백 (Rewards & Flow)**
  - [x] **XP & Level System**: 업무 완료 시 XP 지급 및 레벨업 시스템 (RPG Style)
  - [ ] **Critical Hit**: 업무 완료 시 5% 확률로 포인트/XP 2배 보너스
  - [x] **Visual Celebration**: `canvas-confetti`를 활용한 꽃가루 연출 (Todoist Style)
  - [ ] **Combo System**: 단시간 내 연속 완료 시 콤보 배율 상승 UI
- [ ] **업적 및 배지 (Achievements)**
  - [ ] **Morning Star**: 오전 8시 이전 업무 3개 완료 시 지급
  - [ ] **Fire Extinguisher**: 높은 우선순위 업무를 신속히 처리 시 지급
  - [ ] **태스크 마스터**: 누적 업무 완료 마일스톤 (100, 500, 1000개)
- [ ] **통계 및 대시보드**
  - [x] 사용자 프로필 영역에 포인트, 스트릭, 레벨 정보 실시간 표시
  - [x] **아키텍처 리팩토링**: 서비스 레이어 및 이벤트 버스 도입으로 통계/게이미피케이션 확장성 확보
  - [ ] 전문성 강조를 위한 'Productivity Score' 네이밍 및 UI 검토
- [ ] **Anki 스타일 고도화 통계 (Insights)**
  - [x] 메인 네비게이션에 'Insights' 탭 및 전용 뷰 추가
  - [x] 대시보드 상단 실시간 요약 바(Summary Bar) 구현
  - [ ] Chart.js 기반의 시각화 차트 도입 (피크 타임, 소스별 비중 등)
  - [ ] 백엔드 업무 데이터 집계 API 연동 (일별 완료 수, AI 효율성 등)
  - [ ] 'Daily Glance' 인사이트 요약 문구 생성 로직 (AI 활용)
