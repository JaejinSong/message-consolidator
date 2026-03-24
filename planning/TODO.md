# TODO List

## PC 버전 업무리스트 가독성 개선 (Readability)

사용자가 업무 정보를 더 빠르고 명확하게 파악할 수 있도록 UI/UX를 개선합니다.

- [x] **가독성 중심의 멀티 테마 지원**
  - [x] 다크(Dark) / 라이트(Light) 테마 전환 기능 및 UI 테마 토글 버튼 추가
  - [x] `localStorage`를 이용한 사용자 테마 설정 저장 및 복원
- [x] **행 레이아웃 및 간격 최적화**
  - [x] 행 간격(Gap) 및 수직 패딩 조정을 통한 시각적 여백 확보
- [x] **텍스트 시독성 향상**
  - [x] 업무 내용(Task) 최대 2줄 표시 (`line-clamp`) 및 폰트 크기 조정
  - [x] 방(Room) 이름을 배지(Badge) 형태로 표시하여 구분감 강화
- [x] **시간 표시 지능화 (Relative Time)**
  - [x] 당일 업무에 대해 "방금 전", "n시간 전" 등 상대 시간 표시 도입
  - [x] 과거 업무의 날짜 표기 간소화

---

## 게이미피케이션 (Gamification) - 보상 및 중독성 설계

사용자의 업무 몰입도를 높이기 위해 Duolingo, GitHub 등 성공적인 서비스의 핵심 요소를 벤치마킹하여 도입합니다.

- [x] **DB 스키마 확장 및 마이그레이션**
  - [x] `users` 테이블: `points`, `streak`, `level`, `xp`, `last_completed_at`, `daily_goal` 컬럼 추가 및 업적 테이블 추가
- [x] **연속성 시스템 (Continuity & Streaks)**
  - [x] **데일리 스트릭**: 매일 최소 1개 업무 완료 시 스트릭 유지 (Duolingo Style)
  - [x] **스트릭 프리즈**: 포인트로 구매 가능한 스트릭 보호권 연동 완료 (50p 소모 및 자동 방어)
  - [x] **생산성 히트맵**: GitHub 커밋 그래프 형태의 일일 업무 달성도 시각화 (Insights 탭 적용 완료)
- [x] **보상 및 피드백 (Rewards & Flow)**
  - [x] **XP & Level System**: 업무 완료 시 XP 지급 및 레벨업 시스템 (RPG Style)
  - [x] **Critical Hit**: 업무 완료 시 5% 확률로 포인트/XP 2배 보너스
  - [x] **Visual Celebration**: `canvas-confetti`를 활용한 꽃가루 연출 (Todoist Style)
  - [x] **Combo System**: 단시간 내 연속 완료 시 콤보 배율 상승 UI
- [x] **업적 및 배지 (Achievements)**
  - [x] **조건부 해금 로직**: 특정 조건(누적 완료 수, 레벨 등) 달성 시 업적 자동 발급
  - [x] **태스크 마스터**: 누적 업무 완료 마일스톤 (10, 50, 100개 등 DB 시딩)
  - [x] **업적 전시 UI**: Insights 탭 하단에 획득한 배지 및 진행 상황 그리드 표시 완료
  - [ ] **Morning Star / Fire Extinguisher**: 시간대 및 우선순위 기반 특수 업적 확장
- [ ] **통계 및 대시보드**
  - [x] 사용자 프로필 영역에 포인트, 스트릭, 레벨 정보 실시간 표시
  - [x] **아키텍처 리팩토링**: 서비스 레이어 및 이벤트 버스 도입으로 통계/게이미피케이션 확장성 확보
  - [x] 전문성 강조를 위한 'Productivity Score' 네이밍 및 UI 검토
- [x] **Anki 스타일 고도화 통계 (Insights)**
  - [x] 메인 네비게이션에 'Insights' 탭 및 전용 뷰 추가
  - [x] 대시보드 상단 실시간 요약 바(Summary Bar) 구현
  - [x] 무의존성(Zero-dependency) CSS/JS 기반의 시각화 차트 도입 (히트맵, 시간대별 활동, 소스 비중)
  - [x] 백엔드 업무 데이터 집계 API 연동 (일별 완료 수, 시간대별 분포, 방치된 업무 등)
  - [x] 'Daily Glance' 인사이트 요약 문구 동적 생성 로직 구현 완료
  - [x] Anki 스타일의 시간대별 활동 열지도(Hourly Breakdown) 추가 완료

---

## 사용자 경험 및 데이터 신뢰성 고도화 (UX & Reliability)

- [x] **빈 상태(Empty State) 디자인 개선**
  - [x] 10종 이상의 위트 있는 응원 메시지 보강 및 자연스러운 번역 적용
  - [x] 아이콘 크기(3rem) 및 메시지 폰트 스타일링 최적화
- [x] **데이터 정합성 및 인사이트 지표 보정**
  - [x] '내가 지금 할 일' 지표 산출 시 본인 할당 여부 체크 로직 추가 (SQL)
  - [x] 지표 레이블 가구 개선 (나의 할 일 / 관심이 필요한 일)
- [x] **품질 검증 자동화**
  - [x] Node.js 기반의 핵심 비즈니스 로직(분류, 필터링) 테스트 케이스 보강
  - [x] Empty State 메시지 및 렌더링 리소스 검증 스크립트 추가

- [x] **시스템 안정성 및 데이터베이스 최적화 (Tech Debt)**
  - [x] Go 백엔드 DB 드라이버를 `lib/pq`에서 `pgx`로 마이그레이션 및 파라미터 바인딩 안정화
  - [x] PgBouncer 등 Connection Pooler 환경에서의 Prepared Statement 충돌(`08P01` 에러) 원천 차단 (`ANY` -> `IN` 전환)
  - [x] **SQL 쿼리 아키텍처 리팩토링**: SQL `VIEW` 도입을 통한 컬럼 프로젝션 표준화 및 중복 쿼리 통합 완료
