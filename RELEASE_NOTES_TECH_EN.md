# Release Notes (Tech) - v2.4.4 (2026-03-30 07:01 UTC)

- [FEAT] Just-in-Time (JIT) Translation: Implemented on-demand translation utilizing a `singleflight` mechanism to suppress redundant upstream requests and integrated a reactive loading UI.
- [FEAT] AI Weekly Insight Report: Enabled automated weekly trend analysis within the Insights module, leveraging historical interaction data for high-level summarization.
- [FEAT] Identity Resolution Engine: Introduced an "Ambiguity Safeguard Engine" to resolve entity conflicts and improve data mapping accuracy across disparate communication channels.
- [FIX] AI Response Integrity: Resolved a critical issue causing truncation in AI-generated reports and implemented regression tests to ensure full-text delivery.
- [UI] Responsive Layout Optimization: Refined mobile UI margins and improved the dashboard 'All Clear' logic for better state synchronization and user feedback.
- [OPTIMIZE] Archive Prioritization: Enhanced the archiving logic based on task completion status and standardized the labeling for canceled work items.
- [REFACTOR] Utility Consolidation: Migrated disparate debug and utility tools into a unified `mc-util` package to streamline the backend maintenance workflow.

---

# Release Notes (Tech) - v2.4.3 (2026-03-30 04:30 UTC)

- [FEAT] AI Relationship Graph: Implemented a visualization system to map entity relationships and interaction patterns within the AI Reporting module.
- [FEAT] High-Volume Translation Engine: Introduced batch chunking logic and optimized connection pooling to handle large-scale translation requests efficiently.
- [SYS] Observability Integration: Integrated WhaTap monitoring for real-time telemetry, performance bottleneck detection, and system health observability.
- [SYS] Database Migration: Transitioned core storage to Turso (libsql) to enhance edge performance and resolved critical startup panic issues during driver initialization.
- [REFACTOR] SQL Architecture: Refactored complex data retrieval patterns using Database Views, reducing join complexity and improving query maintainability.
- [UI] Dark Mode Optimization: Stabilized the Insights tab layout and refined markdown CSS for superior readability and icon consistency in dark mode.
- [FIX] Gmail Identity Rendering: Resolved a state synchronization bug where assignees from Gmail-sourced tasks were intermittently rendered as 'undefined'.

---

# Release Notes (Tech) - v2.4.2 (2026-03-30 01:24 UTC)

- [FEAT] Identity Resolution: Implemented "Ambiguity Safeguard Engine" to intelligently resolve user identities across disparate messaging platforms, reducing duplicate entity creation.
- [FIX] AI Response Handling: Resolved analysis truncation issues in LLM outputs and improved Gemini client token usage logging for better cost observability.
- [SYS] Toolchain Consolidation: Unified debug and utility scripts into a centralized `mc-util` package and updated GitHub workflows for improved developer ergonomics.
- [REFACTOR] Gemini Client: Abstracted response text handling and token telemetry into dedicated handlers to enhance testability and modularity.
- [STABILITY] Automated Quality Assurance: Integrated comprehensive regression tests for AI analysis modules to ensure consistency in weekly report generation.
- [UI] Dashboard Logic: Refined "All Clear" state detection logic to provide a more accurate representation of completed task states.
- [SYS] Docker Build Optimization: Updated `.dockerignore` and Dockerfile to support localized release note distribution within containerized environments.

---

# Release Notes (Tech) - v2.4.1 (2026-03-29 12:43 UTC)

- [FEAT] AI Insights Reporting: Launched Weekly AI synthesis engine with LLM-powered summarization and Relationship Graph visualization for team dynamics.
- [SYS] DB Migration to Turso: Migrated primary database to Turso (libsql) to leverage edge-computing benefits and reduce global query latency.
- [REFACTOR] SQL Architecture via Views: Refactored complex multi-table joins into managed database views to improve query maintainability and read performance.
- [SYS] WhaTap Observability: Integrated WhaTap monitoring and Session Replay capabilities for real-time telemetry and advanced production debugging.
- [PERF] Batch Engine Optimization: Implemented translation chunking and optimized DB connection pooling to handle high-volume message processing efficiently.
- [FIX] Assignee Resolution: Resolved "undefined" assignee rendering bug in Gmail tasks by refining the `resolveActualAssignee` context parameters.
- [UI] Mobile Layout Refinement: Optimized mobile UI margins and padding to ensure consistent readability across diverse small-screen devices.
- [STABILITY] CI/CD Verification: Updated `deploy.sh` to mandate `npm test` pre-verification, preventing regressions during the deployment lifecycle.
- [SYS] MC-Util Consolidation: Consolidated disparate debug and utility tools into a unified `mc-util` package for cleaner backend logic.

---

---

# Release Notes - v2.4.0 (2026-03-29 12:15 UTC)

## 🛡️ Self-Healing Identity Resolution & Ambiguity Safeguards

- **[NEW] Self-Healing Identity Resolution**: Implemented a real-time normalization engine that automatically sanitizes fragmented email and Slack identifiers in the `messages` table.
- **[NEW] Ambiguity Safeguards**: Added defensive logic to prevent data contamination when multiple contact matches are detected. Ambiguous entries are flagged with `(Ambiguous)` in UI/Reports.
- **[REFACTOR] Deep Lookup Integration**: Expanded identity lookup queries to search across `canonical_id`, `display_name`, and `aliases` simultaneously, significantly improving resolution accuracy.
- **[FIX] SA6005 Lint Resolution**: Refactored string comparisons to use `strings.EqualFold` for case-insensitive, efficient identity matching.
- **[DOCS] Documentation Normalization**: Corrected erroneous future-dated entries and synchronized the project versioning to `v2.4.0` across all release notes.

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
