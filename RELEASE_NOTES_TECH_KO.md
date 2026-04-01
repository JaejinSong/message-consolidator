# 업데이트 소식 (기술) - v2.4.6 (2026-04-01 06:04 UTC)

- [FEAT] 실시간(JIT) 번역 시스템: `singleflight` 패턴을 도입하여 동시 요청 시 중복되는 API 호출을 방지하고, 필요한 시점에 즉시 번역을 수행하는 엔진을 구현했습니다.
- [FEAT] AI 주간 리포트: Insights AI 모듈 내에 주기적인 커뮤니케이션 분석을 제공하는 주간 리포트 기능을 활성화했습니다.
- [FEAT] 정체성 모호성 방어 엔진: Identity Resolution 시스템에 엔티티 매칭의 모호성이 높은 경우를 대비한 가드레일 로직을 추가하여 데이터 정확도를 높였습니다.
- [OPTIMIZE] WhatsApp 처리 최적화: WhatsApp 메시지 수집 및 작업 추출 로직을 고도화하고, React 마이그레이션을 위한 기본 구조를 반영했습니다.
- [FIX] AI 분석 텍스트 잘림 현상: AI가 생성한 긴 요약문이 중간에 끊기던 현상을 수정하고, 요약 무결성을 보장하기 위한 회귀 테스트를 추가했습니다.
- [UI] 모바일 레이아웃 및 아카이브 개선: 모바일 환경의 여백을 최적화하였으며, 보관 로직을 개선하고 기존 '보관된 작업'의 명칭을 '취소한 업무'로 변경했습니다.
- [SYS] Turso 데이터베이스 이전: 데이터베이스를 Turso(libsql)로 마이그레이션하여 엣지 응답 속도를 개선하고 시스템 시작 시 발생하던 패닉 현상을 해결했습니다.
- [REFACTOR] 도구 통합: 산재해 있던 디버깅 및 유틸리티 도구들을 `mc-util` 패키지로 통합하여 개발 워크플로우를 단일화했습니다.

---

# 업데이트 소식 (기술) - v2.4.5 (2026-04-01 05:29 UTC)

- [FEAT] 정체성 식별 엔진 V3: 채널 간 계정 연동 로직을 고도화하고, 연동 과정에서 발생하던 UI 예외 상황을 해결했습니다.
- [FEAT] 관계도 시각화(Relationship Graph): AI 리포트 시스템 내에 엔티티 간의 상호작용을 파악할 수 있는 그래프 뷰 모듈을 통합했습니다.
- [FIX] Gmail 작업 할당 오류: Gmail 기반 태스크에서 담당자(Assignee)가 'undefined'로 렌더링되던 버그를 수정했습니다.
- [UI] 다크 모드 및 인사이트 안정화: 인사이트 탭의 리포트 가독성을 위한 색상 대비 최적화 및 아이콘 정렬 상태를 개선했습니다.
- [OPTIMIZE] 대량 번역 엔진 최적화: 번역 청킹(Chunking) 및 커넥션 풀링을 적용하여 대규모 메시지 처리 성능을 향상했습니다.
- [SYS] 관측성 강화: WhaTap 모니터링 시스템을 연동하여 실시간 성능 지표 확인 및 에러 트래킹 능력을 강화했습니다.
- [REFACTOR] DB 아키텍처 재설계: SQL View를 활용하여 쿼리 로직을 리팩토링함으로써 백엔드 데이터 접근 계층을 단순화했습니다.
- [REFACTOR] Gemini 클라이언트 표준화: 토큰 사용량 로깅 및 응답 텍스트 처리 방식을 정규화하여 AI 진단 정확도를 높였습니다.

---

# 업데이트 소식 (기술) - v2.4.4 (2026-03-30 07:01 UTC)

- [FEAT] 실시간(JIT) 번역 시스템: `singleflight` 패턴을 적용하여 중복된 번역 요청을 방지하고, 온디맨드 처리를 위한 로딩 UI 상태를 통합했습니다.
- [FEAT] AI 주간 리포트: 인사이트 모듈 내에 주간 업무 흐름을 분석하고 요약해주는 자동 리포트 생성 기능을 활성화했습니다.
- [FEAT] 정체성 식별 엔진: 다양한 채널에서 발생하는 사용자 및 엔티티 혼동을 방지하기 위해 'Ambiguity Safeguard Engine'을 도입하여 데이터 정확도를 개선했습니다.
- [FIX] AI 분석 결과 절단 오류: 리포트 생성 시 응답이 중간에 끊기는 현상을 해결하고, 일관된 결과 출력을 위한 회귀 테스트를 추가했습니다.
- [UI] 모바일 최적화 및 대시보드 개선: 모바일 환경의 마진을 조정하고 대시보드 'All Clear' 로직을 고도화하여 사용자 경험의 반응성을 높였습니다.
- [OPTIMIZE] 아카이브 로직 고도화: 완료 상태에 따른 아카이브 우선순위를 재정의하고, 취소된 항목에 대한 라벨링 시스템을 개선했습니다.
- [REFACTOR] 유틸리티 통합: 분산되어 있던 디버깅 및 유틸리티 도구들을 `mc-util`로 통합하여 백엔드 로직의 유지보수 효율을 강화했습니다.

---

# 업데이트 소식 (기술) - v2.4.3 (2026-03-30 04:30 UTC)

- [FEAT] AI 관계 그래프: AI 리포팅 시스템 내에 엔티티 간의 연결 고리와 상호작용 패턴을 시각화하는 그래프 기능을 도입했습니다.
- [FEAT] 대용량 번역 엔진: 대규모 번역 요청 처리를 위한 배치 청킹(Batch Chunking) 로직을 구현하고 커넥션 풀링 효율을 최적화했습니다.
- [SYS] 관측성(Observability) 통합: 실시간 성능 분석 및 시스템 상태 모니터링을 위해 WhaTap 관제 솔루션을 통합했습니다.
- [SYS] 인프라 마이그레이션: 엣지 환경의 성능 향상을 위해 코어 데이터베이스를 Turso(libsql)로 이전하고, 드라이버 초기화 시 발생하던 패닉 이슈를 해결했습니다.
- [REFACTOR] SQL 아키텍처 개선: 데이터베이스 View를 도입하여 복잡한 Join 연산을 단순화하고 데이터 조회 로직의 유지보수성을 높였습니다.
- [UI] 다크 모드 가독성 강화: 인사이트 탭의 레이아웃을 안정화하고 다크 모드에서의 마크다운 가독성 및 아이콘 렌더링 품질을 개선했습니다.
- [FIX] Gmail 담당자 렌더링 수정: Gmail 소스 작업에서 담당자 정보가 간헐적으로 'undefined'로 표시되던 상태 동기화 오류를 수정했습니다.

---

# 업데이트 소식 (기술) - v2.4.2 (2026-03-30 01:24 UTC)

- [FEAT] 식별 정보 정밀화: 서로 다른 메시징 플랫폼 간의 사용자 식별 모호성을 해결하기 위한 '모호성 보호 엔진(Ambiguity Safeguard Engine)'을 도입하여 중복 엔티티 생성을 억제했습니다.
- [FIX] AI 분석 최적화: LLM 응답이 중간에 끊기는 현상을 수정하고, 비용 가시성 확보를 위해 Gemini 클라이언트의 토큰 사용량 로깅 로직을 개선했습니다.
- [SYS] 도구 통합: 개발 생산성 향상을 위해 파편화된 디버깅 및 유틸리티 도구를 `mc-util` 패키지로 통합하고 관련 워크플로우를 업데이트했습니다.
- [REFACTOR] Gemini 클라이언트 구조 개선: 응답 텍스트 처리 및 토큰 사용량 추적 로직을 분리하여 코드의 테스트 가능성과 모듈성을 강화했습니다.
- [STABILITY] 품질 보증 강화: AI 분석 모듈에 대한 회귀 테스트(Regression Tests)를 추가하여 주간 리포트 생성 로직의 안정성을 확보했습니다.
- [UI] 대시보드 로직 정교화: 'All Clear' 상태 판별 로직을 개선하여 업무 완료 현황이 실제 데이터와 일치하도록 수정했습니다.
- [SYS] 빌드 시스템 수정: 컨테이너 환경에서 다국어 릴리즈 노트를 올바르게 배포할 수 있도록 Docker 설정 및 `.dockerignore` 파일을 업데이트했습니다.

---

# 업데이트 소식 (기술) - v2.4.1 (2026-03-29 12:43 UTC)

- [FEAT] AI 주간 리포트 시스템: LLM 기반의 주간 업무 요약 및 팀 내 협업 관계를 시각화하는 관계 그래프(Relationship Graph) 엔진 도입.
- [SYS] Turso(libsql) DB 마이그레이션: 글로벌 쿼리 지연 시간 단축 및 에지 컴퓨팅 활용을 위해 메인 데이터베이스를 Turso로 이전.
- [REFACTOR] SQL View 아키텍처 도입: 복잡한 조인 쿼리를 DB 뷰(View)로 추상화하여 데이터 접근 로직의 유지보수성과 성능 개선.
- [SYS] WhaTap 모니터링 통합: 실시간 가시성 확보를 위한 WhaTap 모니터링 및 세션 리플레이(Session Replay) 기능 연동.
- [PERF] 배치 처리 엔진 고도화: 대량 메시지 처리를 위한 번역 청킹(Chunking) 도입 및 데이터베이스 커넥션 풀링 최적화.
- [FIX] 담당자 렌더링 오류 수정: Gmail 연동 업무에서 담당자가 'undefined'로 표시되던 로직 결함을 `resolveActualAssignee` 함수 수정을 통해 해결.
- [UI] 모바일 UI 정밀 조정: 모바일 환경에서의 가독성 향상을 위해 여백(Margin) 및 레이아웃 배치 최적화.
- [STABILITY] 배포 안정성 강화: `deploy.sh` 내 `npm test` 검증 단계를 필수화하여 배포 전 회귀 테스트(Regression Test) 수행 보장.
- [SYS] MC-Util 통합: 파편화되어 있던 디버깅 도구 및 유틸리티를 `mc-util`로 통합하여 백엔드 로직 구조화.

---

---

# Release Notes - v2.4.0 (2026-03-29 12:15 UTC)

## 🛡️ 자가 치유형 식별자 정규화 및 동명이인 방어 엔진 도입

- **[NEW] Self-Healing Identity Resolution**: 파편화된 이메일 및 슬랙 식별자를 실시간으로 정규화하고, `messages` 테이블의 데이터를 자동으로 세탁하는 자가 치유 엔진을 구현했습니다.
- **[NEW] Ambiguity Safeguard**: 검색 결과가 2개 이상인 동명이인 상황 발생 시, 데이터 오염을 방지하기 위해 자동 업데이트를 중단하고 `(Ambiguous)` 플래그를 표시하는 방어 로직을 추가했습니다.
- **[REFACTOR] Deep Lookup 강화**: `contacts` 테이블 검색 시 `canonical_id` 뿐만 아니라 `display_name`과 `aliases`까지 통합 검색하도록 쿼리를 확장하여 정규화 성공률을 높였습니다.
- **[FIX] SA6005 린트 수정**: 문자열 비교 로직을 `strings.EqualFold`로 개선하여 대소문자 구분 없는 안전한 비교와 성능 최적화를 달성했습니다.
- **[DOCS] 문서 정규화**: 과거의 잘못된 버전 표기 및 미래 날짜 오기입을 모두 수정하고 프로젝트 버전을 `v2.4.0`으로 동기화했습니다.

---

# Release Notes - v2.3.14 (2026-03-29 09:45 UTC)

- **[FEAT] Advanced Identity Resolution & Relationship Mapping**: Implemented a multi-stage identity resolution engine that prioritizes email identifiers while preserving user-defined aliases. This significantly improves the accuracy of communication network visualizations.
- **[FEAT] Relationship Visualization Graph**: Introduced a dynamic network map in AI Weekly Reports to visualize team interactions and identify communication silos.
- **[OPTIMIZE] Archive Triage Logic**: Enhanced the archive sorting algorithm to prioritize completed tasks and standardized naming for cancelled items to improve dashboard clarity.
- **[PERF] Global Edge Database Migration**: Relocated the primary database to edge infrastructure, reducing latency and improving responsiveness for users worldwide.
- **[I18N] Cross-Project Document Localization**: Standardized multi-language document management for release notes and automated report summaries across all supported locales.

---

# Release Notes - v2.3.13 (2026-03-28 16:30 UTC)

- **[NEW] Multi-source History Merging**: Implemented a transparent `UNION ALL` strategy for fetching messages across active and archived tables, ensuring comprehensive data coverage for AI-generated reports.
- **[I18N] AI Weekly Report Localization**: Added full localization support (KR, EN, ID) for AI-generated insights and trend analysis reports.
- **[UI] Production-ready Insights**: Removed beta placeholders from the Insights tab, enabling full access to real-time productivity metrics.

---

# Release Notes - v2.3.12 (2026-03-28 15:30 UTC)

- **[PERF] Large-scale Task Processing**: Optimized backend reconciliation logic to handle projects with >10,000 active tasks without UI degradation.
- **[FIX] Missing Attribute Sanitization**: Resolved an issue where AI-extracted metadata fields (sender/receiver) could occasionally appear as "null" in the dashboard.

---

# Release Notes - v2.3.11 (2026-03-28 07:10 UTC)

- **[UX] Archive Triage Prioritization**: Reversed the sorting order in the Archive tab to show the most recently completed or cancelled tasks at the top.
- **[UI] Active Status Re-labeling**: Renamed the "Deleted" status to **"Cancelled"** to better reflect the intentionality of task management.
- **[FIX] Archive Filter Precision**: Corrected a logical error in the archive view that occasionally mixed completed and ongoing tasks when filtering by channel.

---

# Release Notes - v2.3.10 (2026-03-27 11:00 UTC)

- **[UI] Redesigned "Empty" States**: Implemented high-fidelity "All Clear" illustrations and motivational messaging for the main dashboard and archive.
- **[REFACTOR] Time Formatting Engine**: Decoupled time localization from the rendering layer and moved it to a centralized utility to ensure consistency across the application.

---

# Release Notes - v2.3.9 (2026-03-27 07:56 UTC)

- **[REFACTOR] Dead Code Elimination**: Pruned 15% of unused legacy templates and CSS styles to reduce bundle size and improve load times.
- **[PERF] Database Connection Pool Tuning**: Optimized connection reuse patterns for Turso to handle bursty concurrent requests more reliably.

---

# Release Notes - v2.3.8 (2026-03-27 01:12 UTC)

- **[FIX] Navigation Bar Persistence**: Resolved a Z-index conflict that caused the user profile and logout buttons to occasionally disappear behind content overlays on high-DPI screens.

---

# Release Notes - v2.3.7 (2026-03-26 02:46 UTC)

- **[FEAT] Real-time Toast Notifications**: Integrated a sleek, non-intrusive notification system (Toast) to provide instant feedback on task operations and system status.
- **[SYS] Hardened Request Validation**: Added defensive middleware to validate incoming webhooks from Slack and WhatsApp, preventing malformed payload errors.

---

# Release Notes - v2.3.6 (2026-03-26 09:15 UTC)

- **[OPTIMIZE] Dashboard Auto-Refresh**: Refined the frontend polling logic to ensure that task completion counts match the actual message states immediately without a full page reload.

---

# Release Notes - v2.3.5 (2026-03-25 10:05 UTC)

- **[NEW] Activity Heatmap Visualization**: Added a GitHub-style activity map to the Insights tab, allowing users to track their message consolidation productivity over time.
- **[PERF] Static Asset Compression**: Enabled Gzip/Brotli compression at the Nginx layer for all JS and CSS assets, resulting in 40% faster initial page loads.

---

# Release Notes - v2.3.4 (2026-03-24 07:15 UTC)

- **[FIX] Gmail Assignee Extraction**: Improved AI prompt engineering to handle complex email threads where assignees are mentioned in the middle of long conversation blocks.
- **[STABILITY] Automatic Schema Migration**: Implemented a startup check that automatically updates database views and indexes to match the latest application requirements.

---

# Release Notes - v2.3.3 (2026-03-24 03:30 UTC)

- **[REFACTOR] SQL View Abstraction**: Introduced `v_messages` and `v_users` views to standardize data retrieval and decouple backend logic from raw table schemas.
- **[SYS] Mandatory Pre-deployment Testing**: Updated `deploy.sh` to require successful completion of both Go backend and JS frontend tests before allowing production deployments.

---

# Release Notes - v2.3.2 (2026-03-24 02:05 UTC)
- **[REFACTOR] Utility Standardization**: Replaced custom date/time utilities with native `Intl` and `Date` APIs for better performance and maintainability.
- **[REFACTOR] Logic Consolidation**: Unified message post-processing logic and error handling across the backend.
- **[STABILITY] Enhanced Error Response**: Standardized error responses and added explicit handling for cancelled requests (HTTP 499).
- **[PERF] Optimized Data Operations**: Improved database row scanning and slice operations using Go 1.21+ `slices` package.
