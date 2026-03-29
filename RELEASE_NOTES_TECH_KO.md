# 업데이트 소식 (기술) - v2.5.0 (2026-03-29 13:00 UTC)

## 🏗️ 아키텍처 리팩토링 및 관측성(Observability) 강화

- **[FEAT] SQL 쿼리 아키텍처 리팩토링**: SQL View를 도입하여 복잡한 비즈니스 로직을 쿼리 계층에서 분리하고, 유지보수성 및 쿼리 실행 성능을 최적화했습니다.
- **[SYS] WhaTap 모니터링 통합**: 실시간 애플리케이션 성능 모니터링(APM) 및 세션 리플레이 분석을 위해 WhaTap 관측성 도구를 시스템에 통합했습니다.
- **[REFACTOR] 백엔드 서비스 격리**: 백엔드 코어 서비스를 프론트엔드 로직으로부터 완전히 격리하고, 산재해 있던 유틸리티 도구들을 `mc-util` 패키지로 통합했습니다.
- **[FIX] Gmail 담당자 렌더링 수정**: Gmail에서 수집된 일부 작업에서 담당자가 `undefined`로 표시되던 렌더링 버그를 해결했습니다.
- **[OPTIMIZE] 배치 번역 엔진 개선**: 대량 번역 시 청크(Chunk) 단위 처리 로직을 구현하고, 커넥션 풀링 최적화를 통해 동시성 처리 능력을 향상했습니다.
- **[PERF] AI 클라이언트 최적화**: Gemini 클라이언트에 토큰 사용량 로깅 및 응답 텍스트 핸들링 로직을 추가하여 분석 결과가 잘리는 현상을 방지했습니다.
- **[SYS] Docker 배포 프로세스 동기화**: 다국어 릴리즈 노트와 자산들이 운영 이미지에 누락 없이 포함되도록 Dockerfile 및 .dockerignore 설정을 업데이트했습니다.
- **[STABILITY] 배포 전 검증 강화**: `deploy.sh` 실행 시 `npm test`를 강제하도록 수정하여, 테스트를 통과하지 않은 빌드가 배포되는 것을 차단했습니다.

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
