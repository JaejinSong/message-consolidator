# Release Notes - v2.3.8 (2026-03-27 01:12 UTC)

## 📊 Analytics & Performance Optimization
- **[FEAT] Anki-style Activity Heatmap**: Introduced a new hourly activity chart inspired by Anki's contribution grid. Users can now visualize productivity density across the 24-hour cycle within the Insights UI.
- **[PERF] Nginx Optimization**: Enabled HTTP/2 protocol and Gzip compression on the reverse proxy layer. This significantly reduces TTFB (Time To First Byte) and improves asset loading speeds for mobile users.
- **[REFACTOR] Gemini Client Abstraction**: Decoupled token usage logging and response text handling within the Gemini AI client. This improves maintainability and prepares the system for multi-modal model expansion.
- **[FIX] Gmail Assignee Rendering**: Resolved a regression where tasks synchronized from Gmail incorrectly displayed "undefined" in the assignee field.
- **[STABILITY] CI/CD Pre-verification**: Updated `deploy.sh` to include mandatory `npm test` execution. The deployment pipeline now automatically aborts if core unit tests fail, ensuring production stability.
- **[UI] Dashboard State Refinement**: Enhanced the 'All Clear' logic on the main dashboard to accurately reflect task completion states across all consolidated channels.
- **[FIX] Navigation UI Consistency**: Fixed a CSS z-index issue that occasionally caused the user profile dropdown and logout button to become invisible or unclickable.

---

# Release Notes - v2.3.7 (2026-03-26 02:46 UTC)

## 🚀 Scalability & Global Data Integrity
- **[FEAT] Database Migration to Turso**: Migrated the primary database architecture to Turso (libsql). This transition enhances edge-computing capabilities and resolves previous startup panic issues, ensuring a more resilient boot sequence.
- **[FEAT] Batch Translation Chunking**: Implemented a chunking algorithm for the translation engine. Large message blocks are now intelligently segmented before processing to prevent API timeouts and ensure reliable multi-language support.
- **[OPTIMIZE] Connection Pooling**: Optimized database connection pooling logic to handle higher concurrent loads, reducing latency during peak message consolidation windows.
- **[FEAT] Global Timezone Support**: Fully migrated time-related schemas to `TIMESTAMPTZ`. This allows for accurate multi-timezone statistics and consistent activity tracking regardless of the user's geographical location.
- **[UI] Real-time Toast Notifications**: Integrated a new toast notification system to provide immediate, non-intrusive feedback for background tasks and system status changes.
- **[REFACTOR] Service Layer Isolation**: Decoupled backend services from utility functions and standardized core logic, improving testability and isolating frontend-specific data transformations.
- **[STABILITY] Frontend Defensive Logic**: Strengthened frontend state management with defensive checks to prevent UI crashes during intermittent network fluctuations or data inconsistencies.

---

# Release Notes - v2.3.6 (2026-03-26 09:15 UTC)

## 🛠️ System Observability & Architectural Refinement
- **[FEAT] WhaTap Integration**: Integrated WhaTap monitoring for real-time server-side observability and performance tracking, including optimized Session Replay logic.
- **[REFACTOR] SQL View Architecture**: Re-engineered SQL query architecture by migrating complex joins and data transformations into Database Views, significantly reducing application-layer complexity.
- **[OPTIMIZE] Dashboard 'All Clear' Logic**: Refined the detection algorithm for the "All Clear" state in the dashboard to ensure immediate and accurate UI updates when all tasks are processed.
- **[REFACTOR] CSS Modularization**: Decomposed monolithic CSS into a modular structure (base, layout, components, responsive) to improve code maintainability and eliminate dead styles.
- **[SYS] Deployment Verification**: Updated `deploy.sh` to mandate `npm test` as a pre-verification step, preventing deployment of regressions to production.
- **[FIX] Assignee Rendering**: Resolved a lingering edge case where assignee fields could render as 'undefined' under specific race conditions during message consolidation.

---

# Release Notes - v2.3.5 (2026-03-25 10:05 UTC)

## 📊 Visual Analytics & Performance Optimization
- **[FEAT] Anki-style Activity Chart**: Implemented a new hourly activity heatmap in the Insights dashboard, inspired by Anki's contribution graph, providing a granular view of message consolidation patterns.
- **[OPTIMIZE] Network Delivery**: Optimized Nginx configuration with HTTP/2 support and Gzip compression. This significantly reduces asset delivery overhead and improves Time to First Byte (TTFB).
- **[UI] Insights Dashboard Redesign**: Overhauled the Insights UI for better data density and aesthetic alignment with the "Premium Dark" theme.
- **[FIX] Layout Regression**: Resolved a CSS z-index issue where the user profile dropdown and logout buttons were rendered inaccessible on specific mobile viewports.
- **[STABILITY] Frontend Defensive Logic**: Enhanced React error boundaries and added defensive checks for statistical data mapping to prevent UI crashes during intermittent API delays.

---

# Release Notes - v2.3.2 (2026-03-24 02:05 UTC)

## 🏗️ Utility Standardization & Backend Logic Consolidation
- **[REFACTOR] Utility Standardization**: Replaced custom date/time utilities with native `Intl` and `Date` APIs for better performance and maintainability.
- **[REFACTOR] Logic Consolidation**: Unified message post-processing logic (`PrepareMessagesForClient`) and error handling (`respondError`) across the backend.
- **[STABILITY] Enhanced Error Response**: Standardized error responses and added explicit handling for cancelled requests (HTTP 499).
- **[PERF] Optimized Data Operations**: Improved database row scanning (`scanMessageRow`) and slice operations using Go 1.21+ `slices` package.

---

# Release Notes - v2.3.1 (2026-03-24 07:55 UTC)

## ⚡ Turso Connection Stability Patch
- **[FIX] "Stream is Closed" Error Mitigation**: Refined the database connection pool settings specifically for Turso/libsql to prevent idle streams from timing out. Set `MaxIdleConns: 2` and shortened `ConnMaxIdleTime` to 30s for better resource recycling.

---

# Release Notes - v2.3.0 (2026-03-24 07:44 UTC)

## 🚀 Database Migration to Turso (libsql) & Stability Fixes
- **[FEAT] Turso DB Infrastructure Migration**: Successfully migrated the primary data store from NeonDB (PostgreSQL) to Turso (libsql/SQLite). This transition leverages global edge distribution for lower latency and improved cost-efficiency.
- **[FIX] Whatsmeow Dialect Compatibility**: Resolved a critical startup panic by mapping the `libsql` driver to the `sqlite3` dialect within the `whatsmeow` SQL store.
- **[SYS] Environment Standardization**: Unified database configuration under `TURSO_DATABASE_URL` and `TURSO_AUTH_TOKEN`, streamlining deployment and management.
- **[DATA] 100% Data Integrity Migration**: Performed a full schema and data migration, ensuring all legacy user content and system states were preserved during the transition.

---

# Release Notes - v2.2.13 (2026-03-23 08:53 UTC)

## ⚡ PgBouncer Compatibility & Batch Retrieval Optimization
- **[FIX] SQL Parameter Binding for PgBouncer**: Resolved "parameter count mismatch" errors (`08P01`) when using `ANY($1)` with `pgx` in pooled environments by transitioning to dynamic `IN` clause generation.
- **[FEAT] Batch Message Retrieval Engine**: Refactored `TranslateMessagesByID` to use `GetMessagesByIDs` for batch loading instead of multiple single queries, significantly optimizing DB I/O.
- **[PERF] Neon DB Connection Strategy**: Re-tuned server settings to `MaxIdleConns: 0` and `MaxOpenConns: 20` to better align with serverless DB traffic patterns and idle resource recovery.
- **[SYS] Centralized Frontend Polling**: Centralized all data refresh intervals into `constants.js` for improved global performance management.

---

# Release Notes - v2.2.11 (2026-03-23 07:08 UTC)

## 🏗️ Backend Refactoring & Driver Migration
- **[REFACTOR] Centralized Task Logic**: Migrated message formatting and original text stripping from handlers to `services/tasks.go` to reduce redundancy.
- **[SYS] DB Driver Migration (pgx)**: Successfully transitioned from `lib/pq` to `github.com/jackc/pgx/v5` for enhanced performance and modern PostgreSQL features.
- **[FIX] SQL Injection Defense**: Replaced raw string formatting with parameterized queries (`$1`, `$2`) across `stats_store.go`.
- **[OPTIMIZE] Token Usage Retrieval**: Removed unnecessary transaction wrapping for simple `QueryRow` calls in `token_store.go`, improving connection pool efficiency.
- **[SYS] Polling Constant Integration**: Standardized frontend polling intervals using the `POLLING_INTERVALS` constant in `app.js`.
- **[FIX] Archive Logic Integrity**: Fixed `safeArchiveDays` calculation to consistently use the system-defined setting.

---

# Release Notes - v2.2.10 (2026-03-23 06:17 UTC)

## 🛠️ Maintenance & Lint Fix
- **[FIX] Gmail API Context**: Resolved a lint issue by properly passing the `context.Context` to the Gmail API `Get` call in `services/tasks.go`.

---

# Release Notes - v2.2.9 (2026-03-23 06:15 UTC)

## 🏗️ Backend Service Extraction & Logic Isolation
- **[REFACTOR] Backend Architecture**: Established a dedicated `services` package to house complex business logic, reducing handler bloat and improving maintainability.
- **[REFACTOR] Logic Isolation**: Isolated pure computational logic (`getDeadlineBadge`, `parseMarkdown`) from `renderer.js` into `logic.js` for better testability and reuse.
- **[IMPROVED] Gmail Channel Parsing**: Modularized `parseNewEmails` and `analyzeAndSaveEmails` in `gmail.go` into smaller, task-specific functions, adhering to the 30-line function rule.
- **[FIX] Linting & Type Integrity**: Resolved multiple variable declarations and type mismatches (`store.TodoItem`) across frontend and backend modules.

---

# Release Notes - v2.2.8 (2026-03-23 04:56 UTC)

## 🕒 Improved Time Readability & Premium Status Icons
- **[IMPROVED] Dynamic Time Formatting**: Refined the time display logic to show relative time (e.g., "5m ago") for recent tasks, day of week for tasks within 7 days, and compact date/time for older tasks.
- **[NEW] Premium Status Icons**: Redesigned the "Stale" and "Abandoned" icons with modern SVG paths and glassmorphism styling.
- **[NEW] Status Glow & Animations**: Added a subtle glow to stale tasks and a smooth pulse animation to abandoned tasks to improve visual urgency and professional aesthetics.
- **[I18N] Localized Day Names**: Added support for day names in KR, EN, ID, and TH languages.

---

# Release Notes - v2.2.7 (2026-03-23 04:50 UTC)

## 🏆 Premium Achievements UI & Multi-language Support
- **[NEW] Premium Achievement Design**: Redesigned the achievement cards with a sleek glassmorphism style, golden glow animations for unlocked milestones, and improved progress bars.
- **[NEW] Global Achievements (i18n)**: Achievement titles and descriptions are now fully localized in **Korean, English, Indonesian, and Thai**.
- **[FIX] Achievement Sync**: Corrected the mapping between database achievement keys and localized UI strings to ensure all trophies display their correct names.

---

# Release Notes - v2.2.6 (2026-03-22 17:35 UTC)

## 🔍 Comprehensive Monitoring with WhaTap
- **[NEW] WhaTap Go Agent Integration**: Implemented deep backend monitoring for real-time performance tracking and SQL execution analysis.
- **[NEW] Real User Monitoring (RUM) & Session Replay**: Integrated WhaTap Browser Agent with **100% Session Replay** for effective frontend debugging.
- **[SYS] Infrastructure & DB Stability**: 
    - Resolved Nginx CSP violations for the Browser Agent.
    - Reinforced NeonDB connection pooling for serverless environments.
- **[NOTICE] Resource Usage**: Added monitoring agents result in an approximate **150MB** increase in memory usage.

---

# Release Notes - v2.2.5 (2026-03-22 14:05 UTC)


## 💎 Better Deployment & Smarter Insights
- **[SYS] Automated Pre-deployment Verification (`deploy.sh`)**: Introduced a pre-verification step (Step 0) in the deployment pipeline to run `go build`, `verify_logic.js`, and `verify_renderer.js` before building Docker images. This prevents broken code from being deployed.
- **[FEAT] Improved Insights UI & Tooltip Clarity**: Enhanced the visual prominence of the "Pending Tasks" card and refined the heatmap tooltip formatting for better data readability.
- **[FIX] Test Script Compatibility**: Resolved a DOM dependency issue in the Node.js verification tools by strictly mocking browser globals, ensuring reliable pre-commit checks.

---

# Release Notes - v2.2.4 (2026-03-22 13:20 UTC)

## 🌍 Global Consistency & Timezone-Aware Analytics
- **[NEW] User-Local Timezone Analytics**: The Insights dashboard now respects your local timezone (`X-Timezone` header). Daily completion charts and hourly activity heatmaps will accurately reflect your local productivity patterns, regardless of your geographic location.
- **[NEW] Expanded Multilingual support for Tags**: Task status tags like `Waiting...` and `My Promise` are now fully translated into English, Indonesian, and Thai, providing a more localized experience for global teams.
- **[REFACTOR] Database Schema Timezone Integrity (TIMESTAMPTZ)**: Migrated all core date/time columns to `TIMESTAMPTZ` to ensure point-in-time integrity across different server and client regions. This includes a re-optimized `idx_messages_archive_sort` index for better performance.
- **[FIX] Archive Sorting Precision**: Refined the sorting logic for archived messages using composite conditions (based on `is_deleted`, `created_at`, or `completed_at`), ensuring your history is always perfectly ordered.

---

# Release Notes - v2.2.3 (2026-03-22 12:45 UTC)

## 💎 Elegant Toast Notifications & Frontend Robustness
- **[NEW] Glassmorphism Toast System**: Replaced intrusive browser alerts with a sleek, non-blocking toast notification system for success and error messages.
- **[IMPROVED] Defensive Frontend Rendering**: Hardened all rendering logic to gracefully handle varied API response structures (arrays, objects, or empty states), ensuring the UI remains stable even during backend data shifts.
- **[IMPROVED] Alias Mapping Visualization**: Updated 'Group Same Person' and 'Auto Name Formatting' lists to clearly show mapping relationships using arrow (→) indicators.
- **[OPTIMIZE] Token Badge UX**: Simplified the token usage badge by removing redundant click alerts and focusing on instant information through hover tooltips.
- **[SYS] Automated Quality Verification**: Expanded the Node.js test suite (`verify_renderer.js`) to mock browser environments, enabling comprehensive testing of UI-dependent logic before deployment.

---

# Release Notes - v2.1.5 (2026-03-21 13:10 UTC)

## 🏗️ Architectural Refactoring for Insights & Gamification
- **[REFACTOR] Backend Service Layer**: Introduced a `services` package to decouple business logic (XP, Streaks, Task Completion) from the data-access layer (`store`) and API handlers.
- **[REFACTOR] Frontend Event-Driven Architecture**: Implemented a central event bus (`events.js`) to decouple core application actions from UI-specific side effects like animations and statistics updates.
- **[OPTIMIZE] Modular UI Rendering**: Refined `renderer.js` logic to extract reusable calculation utilities, preparing the foundation for the upcoming 'Insights' dashboard.

---

# Release Notes - v2.1.4 (2026-03-21 12:10 UTC)

## 💎 Real-time Resource Updates & UI Refinement
- **[FEAT] UI Token Real-time Update**: Token usage status is now synchronized in real-time, providing immediate visibility into AI resource consumption without manual refresh.
- **[UI] DASHBOARD FINE-TUNING**: Polished various UI components for a more premium and professional aesthetic across the main dashboard.

---

# Release Notes - v2.1.3 (2026-03-21 11:10 UTC)

## ⚡ Optimized Deployment & Slack API Pagination
- **[FEAT] Optimized VPS Deployment**: Implemented a modern deployment pipeline using Google Artifact Registry and GCS. Images are now size-optimized using `-ldflags="-s -w"` and `upx` compression (~10MB total).
- **[OPTIMIZE] Slack API Pagination Support**: Enhanced `GetMessages` in `SlackClient` to handle more than 100 messages using recursive cursor-based pagination, ensuring no missing history during peak activity.
- **[FIX] Slack Scanner Reliability**: Fixed a compilation error in `SlowSweeper` and resolved an invalid nil check for `ResponseMetaData` in the Slack client to ensure stable background scanning.

---

# Release Notes - v2.1.2 (2026-03-21 09:55 UTC)

## 📱 Mobile UI Optimization & Layout Stabilization
- **[UI] Responsive Layout Enhancement**: Overhauled media queries for 480px and 768px (Tablet/Landscape) to eliminate content overflow and "cutting off" issues in the header.
- **[UI] Header Element Stacking**: Implemented a vertical stacking strategy for Logo, Gamification Stats, and Utility groups on narrow screens, using `space-evenly` for improved visual balance.
- **[UI] Integration Status Grid**: Rearranged Slack, WhatsApp, and Gmail status icons into a 3-column grid, improving legibility and touch target size on mobile.
- **[FIX] XP Animation Positioning**: Refined the position calculation logic for XP gain animations to ensure they remain fully contained within the mobile viewport.
- **[SYS] Go Dependency Cleanup**: Explicitly defined direct dependencies like `golang.org/x/sync` via `go mod tidy`.

---

# Release Notes - v2.1.1 (2026-03-21 08:48 UTC)

---

# Release Notes - v2.1.0 (2026-03-21 08:45 UTC)

## ⚡ Monthly Token Usage & Graceful Shutdown
- **[FEAT] Monthly Token Usage Tracking**: Added monthly cumulative token usage aggregation and display. The token badge tooltip now shows both daily and monthly totals with cost estimates.
- **[FEAT] Graceful Shutdown Implementation**: Implemented a robust server shutdown sequence in `main.go`. The server now handles `SIGINT` and `SIGTERM` to safely flush in-memory data (tokens, metadata), disconnect WhatsApp sessions, and close database connections before exiting.

---

# Release Notes - v2.0.9 (2026-03-21 07:48 UTC)

## ⚡ Slack API Optimization & Scanner Architecture Refactoring
- **[OPTIMIZE] Slack Rate Limit Handling**: Implemented smart retries for Slack API calls. When a `Rate Limit(HTTP 429)` is encountered, the scanner now waits for the exact `Retry-After` duration and retries up to 3 times before failing.
- **[REFACTOR] Scanner Package Migration**: Successfully migrated monolithic scanner logic from `main.go` to a dedicated `scanner` package, significantly improving code modularity and maintainability.
- **[CLEANUP] Redundant Code Removal**: Deleted the legacy `scanner.go` from the root directory to align with the new package-based architecture.

---

# Release Notes - v2.0.8 (2026-03-21 07:07 UTC)

## ⚡ Intelligent DB Connection Management
- **[FEAT] Automated DB Provider Detection**: Added logic to distinguish between Neon DB and standard PostgreSQL.
- **[OPTIMIZE] Dynamic Pooling Strategy**: 
    - **Neon DB**: Auto-configures `MaxIdleConns(0)` for optimal scale-to-zero efficiency.
    - **Standard PostgreSQL**: Maintains a minimum pool of `MaxIdleConns(2)` for reduced latency.

---

# Release Notes - v2.0.7 (2026-03-20 18:30 UTC)

## 🎮 Gamification Infrastructure & DB Resource Cleanup
- **[PLAN] Gamification Roadmap**: Outlined points, streaks, and leveling systems in `planning/TODO.md`.
- **[OPTIMIZE] DB Performance Layer**: Initial scale-to-zero configuration applied. (Refined in v2.0.8)
- **[OPTIMIZE] Consolidated DB Pool**: Merged the WhatsApp storage pool into the primary application pool to minimize connection overhead.

---

# Release Notes - v2.0.6 (2026-03-20 17:40 UTC)

## ⚡ Bulk Operations & Performance Optimization
- **[FEAT] Batch Processing Engine**: Implemented single-query batch patterns for massive throughput gains in `Save`, `Delete`, and `Restore` operations.
- **[OPTIMIZE] Scanner Performance**: Refactored Slack, WhatsApp, and Gmail scanners to leverage batch saves, improving scan speed and reducing DB overhead.
- **[FEAT] On-Demand Resource Loading (Lazy Loading)**: Successfully decoupled `original_text` from the main payload. Message source text is now fetched only when requested, drastically reducing memory footprint and initial page load size.
- **[FIX] API Endpoint**: Added `/api/messages/{id}/original` for authenticated on-demand fetching.

---

# Release Notes - v2.0.5 (2026-03-20 17:25 UTC)

## ⚡ On-Demand Original Message Fetching (Lazy Loading)
- **[FEAT] Token Optimization**: Removed `original_text` from AI extraction. The system now preserves raw message text during scanning and fetches it only when requested by the user.
- **[OPTIMIZE] Payload Reduction**: Active dashboard and archive lists no longer include full original text by default, significantly reducing initial page load size.
- **[FIX] API Implementation**: Added `/api/messages/{id}/original` endpoint for authenticated lazy loading of message source text.

---

# Release Notes - v2.0.4 (2026-03-20 17:15 UTC)

## ⚡ Token Optimization & Prompt Streamlining
- **[FEAT] Reduced AI Cost**: Optimized system prompts in `ai/gemini.go` to be more concise while maintaining extraction accuracy.
- **[FEAT] Cost-Efficient Model Selection**: Defined and implemented a tiered model selection strategy (Lite for translation, Flash for analysis) in `GEMINI.md`.

---

# Release Notes - v2.0.3 (2026-03-20 17:15 UTC)

## 🛠️ Build System Hotfix
- **[FIX] Dependency Version Mismatch**: Resolved a critical build error in `generative-ai-go` caused by a proto version conflict. Downgraded to a stable dependency set (`genai@v0.13.0`, `api@v0.186.0`) to restore buildability.

---

# Release Notes - v2.0.2 (2026-03-20 17:15 UTC)

## ⚡ Backend Cache Optimization & Archive Restore Fixes
- **[NEW] Unified Cache Refresh Strategy**: Refactored `MarkMessageDone`, `DeleteMessage`, `HardDeleteMessage`, `UpdateTaskText`, and `UpdateTaskAssignee` to use a centralized `RefreshCache` mechanism, ensuring 100% data consistency across all views.
- **[NEW] High-Performance Prepend**: Optimized `SaveMessage` by directly prepending new tasks to the in-memory cache, providing near-instant UI feedback for new entries.
- **[FIX] Archive Restore Reliability**: Fixed a bug where restored messages would remain hidden or fail to appear in the active dashboard due to cache misses and status conflicts.
- **[FIX] Slack Status Accuracy**: Improved Slack connection detection by verifying token existence via a dedicated status endpoint.

---

# Release Notes - v2.0.1 (2026-03-20 16:35 UTC)

## 🏗️ Frontend Modularization & UI Logic Fixes
- **[NEW] Frontend Architecture Refactoring**: Refactored monolithic frontend logic into clean, domain-specific modules (`taskFilter.js`, `icons.js`, `archive.js`, `modals.js`) to improve maintainability and scalability.
- **[FIX] Modal Theme Mismatch**: Resolved a critical readability issue in the light theme by ensuring the details modal uses theme-aware background colors.
- **[FIX] Empty State Visibility**: Corrected the logic to ensure witty messages appear correctly for each section (My Tasks / Other Tasks) independently.

---

# Release Notes - v2.0.0 (2026-03-20 15:50 UTC)

## ☁️ Google Cloud Run Serverless Migration
- **[NEW] Serverless Optimization**: Introduced `CLOUD_RUN_MODE` to disable background scanners and use API-triggered scanning for optimal serverless execution.
- **[NEW] Internal Scan API**: Added `/api/internal/scan` for external triggers (e.g., Cloud Scheduler) with `X-Internal-Secret` security.
- **[NEW] Anti-Resonance Jitter**: Implemented 0-5s random delays to distribute load across concurrent instances.
- **[NEW] Automated Deployment Workflow**: Created `cloud-run-deploy.sh` and `cloud-scheduler-setup.sh` for full serverless pipeline automation.
- **[FIX] Dynamic Port Binding**: Updated server startup to respect the `$PORT` environment variable.

---

# Release Notes - v1.9.7 (2026-03-20 15:00 UTC)

## 🎊 Witty Empty States & Emotional UX Enhancement
- **[NEW] Witty Empty States**: Added 10+ random witty messages per language (KO, EN, ID, TH) that appear when "My Tasks" are complete.
- **[UI] Polished Empty View**: Implemented a new design with subtle animations and borders for a more engaging "Zero Tasks" experience.

---

# Release Notes - v1.9.6 (2026-03-20 14:40 UTC)

## 🌐 Advanced I18n & Deployment Automation
- **[NEW] Global Expansion**: Enhanced multilingual support with Indonesian (ID) and Thai (TH) locales, including localized tooltips for a seamless global experience.
- **[NEW] Automated Deployment Pipeline**: Introduced `deploy.sh` to automate local building, GCS uploading, and remote VPS updates in a single command.
- **[UI] Tab Visibility Tuning**: Refined active tab contrast in the light theme for better navigation clarity.
- **[SYS] Store Logic Stabilization**: Improved data store and cache handling to ensure consistency during concurrent synchronization.

---

# Release Notes - v1.9.5 (2026-03-19)

## ⚡ Archive Search & Performance Optimization
- **[NEW] Full-Text Search Optimization**: Implemented GIN Trigram indexes across all key searchable fields of the archive.
- **[NEW] Assignee-Based Filtering**: Added a dedicated assignee filter to help users find specific owner tasks quickly among thousands of archived items.
- **[SYS] Database Scalability**: Introduced functional and partial indexes to maintain near-instant sorting and auto-archival performance for massive datasets.

---

# Release Notes - v1.9.4 (2026-03-18)

## 🎨 Premium Light Theme & UI Polish
- **[UI] Slate & Deep Indigo Palette**: Introduced a high-contrast premium light theme for better text legibility and modern aesthetics.
- **[UI] Active Tab Visibility**: Enhanced active tab text to white on a purple background to highlight the current view clearly.
- **[UI] Dashboard Icon Overhaul**: Redesigned Slack, WhatsApp, and Gmail icons with better borders and drop shadows for refined visibility.

---

# Release Notes - v1.9.3 (2026-03-17)

## 🛠️ Export Reliability & Layout Stabilization
- **[FIX] Force Download Handler**: Updated export handlers to use `attachment` disposition, ensuring direct downloads across all browsers.
- **[FIX] Table Header Alignment**: Replaced "SOURCE" text with a compact "#" icon and tooltip to prevent header overflow and layout breaking.

---

# Release Notes - v1.9.2 (2026-03-16)

## 🚀 Header Grid & UX Performance
- **[NEW] 3-Segment Grid Header**: Restructured header layout into Branding, Navigation, and Utility sections for improved balance.
- **[NEW] Hardware-Accelerated Transitions**: Applied GPU acceleration (`will-change`, `translateZ`) to all UI transitions for buttery-smooth interactivity.
- **[NEW] Mobile-Adaptive Headings**: Implemented CSS Grid areas to optimize layout automatically for 1024px, 768px, and 480px widths.

---

# Release Notes - v1.9.1 (2026-03-20 07:15 UTC)

## 🎨 User Settings UI & I18n Refinement
- **[NEW] Tabbed Settings Layout**: Refactored the settings modal with a sidebar navigation, separating "General" and "Name Rules" for improved focus and readability.
- **[NEW] Purpose-Oriented UX**: Renamed technical terms like "Normalization" to user-friendly titles like "Group Same Person" and "Auto Name Formatting" with intuitive descriptions.
- **[NEW] Mobile-Optimized Settings**: Implemented a responsive design where the sidebar transforms into a horizontal tab bar on small screens, ensuring full accessibility on mobile.
- **[NEW] Deep I18n Integration**: Fully localized all new settings elements, placeholders, and tooltips in both Korean and English.
- **[FIX] Contextual Mapping Flow**: Improved the "Quick Mapping" experience by auto-populating mapping fields when navigating from the task list.

---

# Release Notes - v1.9.0 (2026-03-20 05:15 UTC)

## 🏗️ Major Architecture Refactoring & Modularization
- **[NEW] Package-Based Structure**: Reorganized the monolithic root directory into clean, domain-driven packages: `ai`, `auth`, `channels`, `config`, `handlers`, `logger`, `store`, and `types`.
- **[NEW] Refined Gmail Scanning**: Switched from `is:unread` to a more robust time-based query (`in:inbox after:timestamp`), ensuring comprehensive synchronization across multiple scans.
- **[NEW] WhatsApp Integration Cleanup**: Introduced top-level wrapper functions for status and QR code retrieval, significantly simplifying the handler layer and reducing coupling.
- **[FIX] Circular Dependency Resolution**: Decoupled package interactions using callbacks and unified storage interfaces, resulting in a more stable and maintainable codebase.

---

# Release Notes - v1.8.5 (2026-03-20 04:23 UTC)

## 📧 Gmail Thread Analysis Refinement
- **[NEW] Thread-Aware Extraction**: Added a critical instruction to Gemini to focus ONLY on the latest message in an email thread, effectively preventing duplicate task extraction from quoted replies or forwarded messages.

---

# Release Notes - v1.8.4 (2026-03-20 04:15 UTC)

## 📱 WhatsApp Name Extraction & Mention Resolution
- **[NEW] Real-time Mention Resolution**: Automatically resolves `@number` mentions to human-readable names (e.g., `@Andy Phan`) before AI analysis, ensuring accurate task assignment.
- **[NEW] Auto-Contact Discovery**: Senders' names and phone numbers are now automatically mapped and cached in the `contacts` table for persistent name resolution.
- **[FIX] Robust Name Preservation**: Refined Gemini extraction prompts to preserve full names including parentheses and non-Latin characters (梁威浩, 박요셉, etc.).
- **[FIX] Name Normalization Fallback**: Enhanced the name normalization layer to resolve phone numbers to names using the newly established contact directory.

---

# Release Notes - v1.8.3 (2026-03-20 04:05 UTC)

## 📢 UI Enhancements & Quick Alias Mapping
- **[NEW] Release Notes UI Integration**: Added a 📢 **Updates** button to the dashboard to view the latest improvements directly within the app.
- **[NEW] Quick Alias Mapping from UI**: Click any requester or assignee name in the task list (including the Archive) to quickly map it to a canonical name in settings.
- **[NEW] Multi-Alias Support**: Updated alias management to allow adding multiple aliases at once using comma separation.
- **[SEC] Global XSS Protection**: Implemented HTML escaping for all dynamically rendered text (tasks, rooms, names) across the dashboard and archive for enhanced security.
- **[UI] Robust Markdown Rendering**: Developed a custom, lightweight markdown-to-HTML formatter specifically for the release notes modal.
- **[API] Release Notes Endpoint**: Created a new server-side handler `/api/release-notes` to serve the user-facing release notes.

---

# Release Notes - v1.8.2 (2026-03-20 03:41 UTC)

---

# Release Notes - v1.8.1 (Old)

## 🎯 Gmail Attribution Refined (Initial)
- **Email Recipient Filtering**: Re-engineered task attribution for Gmail based on `To/CC/BCC` headers.
- **Remediation API**: Added `/api/admin/restore-gmail-cc` for retroactive cleanup.


# Release Notes - v1.8.0 (Old)

# Release Notes - v1.7.4 (Old)

## 🏗️ Data Model Unification & I18n Refactoring
- **RawMessage Consolidation**: Unified Slack, Gmail, and WhatsApp into a single `RawMessage` struct, removing redundant fields (`User`, `RawTS`) for a leaner data model.
- **`WAManager` Struct Implementation**: Refactored WhatsApp integration into a dedicated manager with callbacks (`OnConnected`, `FetchUserWAJID`), decoupling it from the `store` package.
- **I18n Architecture Overhaul**: Moved UI text to `locales.js` and simplified `i18n.js` to use `data-i18n` attributes, improving maintainability and reducing JS bundle overhead.
- **Enhanced Error Observability**: Upgraded several logs from `Debug` to `Warn/Error` across all scanner and handler services, adding rich context (user email, channel ID) for faster troubleshooting.
- **NeonDB Cold Start Resilience**: Implemented `WithDBRetry` with exponential backoff (2s, 4s, 6s) to gracefully handle serverless database wake-ups.
- **Connection Pool Tuning**: Optimized `MaxIdleTime` (1m) and `MaxOpenConns` (20) specifically for Neon's autosuspend behavior, ensuring clean scale-to-zero while maintaining high burst readiness.
- **Gmail Timestamp Precision**: Updated `AssignedAt` logic to use actual email receipt time (`InternalDate`) for better historical accuracy.
- **Deployment Size Optimization**: Binary is now stripped and compressed with UPX, reducing container image size by ~70%.

---

# Release Notes - v1.7.3 (Old)

## ⚡ Architectural Refinement & Background Scanner Optimization
- **Multithreaded Background Scanning**: Parallelized Gmail, Slack, and WhatsApp sources for every user, significantly reducing overall heartbeat duration.
- **DRY API Handlers**: Unified handler logic with `decodeJSON`, `respondJSON`, and `applyTranslations` helpers, improving maintainability and ensuring proper resource cleanup.
- **Gmail & Slack Modularization**: Extracted source-specific logic into modular helpers for clearer traceability and future extensibility.
- **Anti-Resonance Scheduler**: Adjusted scanner interval to 59s to prevent execution alignment with other periodic system tasks.
- **High-Performance Classification**: Optimized alias/mention detection logic with pre-calculated lowercasting and unified loops.

---

# Release Notes - v1.7.2 (Old)

## 🎨 UI Performance & Asset Optimization
- **Static Asset Minification**: Integrated `tdewolff/minify` into the Docker build process and `Makefile`. All HTML, CSS, and JS files are now automatically minified during deployment, reducing payload size and improving load times.
- **Rendering Overhead Reduction**: Optimized `style.css` by reducing heavy visual effects like extreme background blurs and deep shadows, resulting in smoother scrolling and lower CPU/GPU usage on the client side.
- **Improved Modal Responsiveness**: Refined modal backdrop and content animations for a snappier user experience.
- **Build Speed Optimization**: Refactored `Dockerfile` with optimized layering. Introduced separate copying for static assets to leverage Docker's build cache, skipping minification if static files are unchanged.
- **WhatsApp Connectivity & Stability**: 
    - **Deep Concurrency Optimization**: Refactored event handlers to process message extraction before acquiring write locks, significantly reducing contention.
    - **Fail-Safe Connection Strategy**: Added 5-attempt retry logic for store initialization to handle DB cold starts or transient latency.
    - **QR Channel Reliability**: Re-ordered connection steps to ensure stable QR code generation and subscription.

---

# Release Notes - v1.7.1 (Old)

## ⚡ Neon DB Sleep & Persistence Optimization
- **Intelligent Metadata Persistence**: Optimized `PersistAllScanMetadata` to only trigger database connections when actual changes exist (`dirtyScanKeys`), allowing Neon DB to remain in sleep mode during idle periods.
- **High-Efficiency Scanning**: Switched to iterating over `dirtyScanKeys` instead of the entire `scanCache` during persistence, significantly reducing CPU overhead for users with large metadata sets.
- **Concurrency-Safe Updates**: Implemented a "version-check" defense mechanism to prevent race conditions. The system now verifies if a cached value has changed during the database write window before clearing its "dirty" flag, ensuring no scan updates are ever lost.

---

# Release Notes - v1.7.0 (Old)

## 🏗️ Package Refactoring & Modularization
- **`store/` Package**: Extracted all storage logic from the monolithic `store.go` into a dedicated `store/` package containing specialized files (`db.go`, `message_store.go`, `user_store.go`, `token_store.go`, `types.go`, `cache_store.go`, `scan_store.go`, `translation_store.go`), significantly improving readability and maintainability.
- **`logger/` Package**: Centralized all application logging into a dedicated `logger/` package, making logging consistent and easily configurable across all modules.
- **Type Consolidation**: Moved all shared data types (`ConsolidatedMessage`, `User`, `TodoItem`, `TranslateRequest`, etc.) into `store/types.go` as the single source of truth.

## 🔤 Consistent Name Normalization
- **Alias-Based Name Normalization**: Implemented `NormalizeName` to map various representations of the same person (e.g., `"YOSEP PARK"`, `"박요셉"`) to a single primary name using user/tenant-defined aliases.
- **Tenant Alias Support**: Per-tenant alias tables allow each user to define their own name mappings for their workspace context.
- **Consistent Requester/Assignee Display**: Normalization is applied when saving messages, so requester and assignee fields always reflect the canonical name.

---

# Release Notes - v1.6.6 (Old)

## 🌐 Language & Cache Reliability
- **Language Transition Fix**: Resolved a critical issue where switching to Korean was incorrectly skipped. Users can now transition between any supported languages seamlessly.
- **Zero-Pollution Rendering**: Implemented defensive slice copying in API handlers to ensure that translating a message for one user doesn't pollute the global server-side cache for others.

## ⚡ Performance Optimization
- **Parallel Scanning**: Slack, WhatsApp, and Gmail background scans are now executed concurrently, reducing total scan time from ~5s to **under 1 second**.
- **Batch Translation Queries**: Optimized database interaction by replacing N+1 translation lookups with a high-performance `GetTaskTranslationsBatch` query, significantly lowering DB connections and latency.

---

# Release Notes - v1.6.5 (Old)

## 🛠️ Infrastructure & Data Reliability
- **Migration & Data Guard**: Fixed a critical "duplicate key value" error in the database migration process. Added a pre-migration cleanup step in `store.go` to ensure a smooth transition when assigning user emails to legacy data.
- **Improved Performance with Gemini Flash Lite**: Updated the translation engine to `gemini-3.1-flash-lite-preview`, optimizing for faster response times and significantly reducing token costs while maintaining high-quality Korean translations.
- **Enhanced VPS Monitoring Tools**: Introduced `vps-logs.sh` and `vps-log-file.sh` utility scripts. These tools allow developers to monitor application logs directly from the development environment, simplifying troubleshooting on the production VPS.
- **Localized Startup Indicator**: Updated the service boot-up message to "기동 완료" to provide clearer confirmation of system readiness during deployment.

---

# Release Notes - v1.6.4 (Old)

## 👤 Sender-Aware Task Classification
- **Self-Initiated Task Recognition**: Improved the classification logic to automatically categorize tasks as **"My Tasks"** if the sender is the user or one of their aliases.
- **Context-Free Attribution**: Tasks sent by the user in public channels are now correctly attributed to them even if their name is not explicitly mentioned in the message text.
- **Cross-Channel Support**: This enhancement applies to both Slack and WhatsApp message sources.

---

# Release Notes - v1.6.3 (Old)

## 🧹 Codebase Cleanup & Optimization
- **Backend Refactoring**: Removed redundant helper functions and unused variables in `gmail.go` and `whatsapp.go` to improve code clarity.
- **Improved Configuration Consistency**: Standardized database connection handling in the WhatsApp module to use centralized configuration (`cfg.NeonDBURL`), ensuring more reliable connectivity.
- **Dependency Optimization**: Performed a thorough dependency audit and cleanup using `go mod tidy` to ensure a minimal and efficient build.
- **Enhanced Maintainability**: Eliminated "dead code" (populated but unread variables) to reduce cognitive load for future development.

---

# Release Notes - v1.6.2 (Old)

## 🪵 Service Startup Indicator
- **Startup Complete Log**: Added a specific "Startup Complete" log message in English to confirm when the database connection, metadata caching, and background workers are fully initialized.
- **Enhanced Deployment Verification**: Updated the VPS deployment workflow (`deploy.md`) to include proactive verification of the "Startup Complete" log, ensuring the service is operational after each update.

---

# Release Notes - v1.6.1 (Old)

## 👤 Intelligent Assignee Detection
- **Smart Assignee Extraction**: Upgraded the extraction logic to prioritize actual names or email recipients found within messages instead of generic "My Task" or "Other Task" labels.
- **Improved Source Consistency**: This logic now applies across all supported channels (Gmail, Slack, and WhatsApp).
- **Fallback Logic**: Maintained standard classification as a fallback if the AI cannot confidently identify a specific person, ensuring "My Tasks" filtering remains robust.

---

# Release Notes - v1.6.0 (Old)

## ⚡ Archive Performance & UX Optimization
- **High-Speed Server-Side Sorting**: Implemented dynamic sorting for all Archive columns (Source, Room, Task, Requester, Assignee, Time, Completed At) directly in SQL for maximum efficiency.
- **NeonDB Compound Indexes**: Created specialized compound indexes `idx_messages_archive_sort_created` and `idx_messages_archive_sort_completed` to ensure near-instant sorting even with massive historical datasets.
- **Improved UI Responsiveness**: Added a sleek loading overlay and spinner to the Archive view, providing immediate visual feedback during data fetching and re-sorting.
- **Icon-Driven Channel Display**: Replaced text-based channel names with modern SVG icons (Slack, WhatsApp, Gmail) in the Archive table, saving horizontal space and improving visual consistency with the main dashboard.
- **Flexible Sorting UI**: Added interactive sort indicators (↑/↓) to table headers, allowing users to easily toggle between ascending and descending orders.

---

# Release Notes - v1.5.0 (Old)

## 🏗️ Structural Refactoring & Performance Optimization
- **Code Modularization**: Refactored the monolithic `main.go` into specialized modules (`handlers.go`, `scanner.go`, `logger.go`, `types.go`) for vastly improved maintainability and readability.
- **Gemini 3 Flash Preview**: Upgraded the AI engine to `gemini-3-flash-preview`, offering cutting-edge performance and responsiveness.
- **Translation Caching**: Introduced a dedicated `task_translations` table in PostgreSQL to cache AI-generated translations, resulting in near-instant language switching for recurring tasks.
- **Prompt Engineering**: Transitioned to `SystemInstruction` API for Gemini calls, optimizing token usage and ensuring more consistent and reliable task extraction.
- **Configuration Management**: Created `GEMINI.md` to formally document and manage AI model preferences.

---

# Release Notes - v1.4.0 (Old)

## 🗄️ Archive Enhancements & Search Optimization
- **Advanced Archive Search**: Implemented case-insensitive search across tasks, rooms, requesters, and original text using `ILIKE` for better historical data retrieval.
- **Efficient Pagination**: Added server-side pagination (limit/offset) to the Archive view, ensuring snappy performance even with thousands of archived messages.
- **Excel (.xlsx) Export**: Integrated `excelize/v2` to support high-quality Excel exports, solving potential encoding issues with CSV and providing better formatting.
- **Robust Multi-Browser Download**: Resolved a critical issue where Chrome would rename downloads to a UUID. Implemented a `Blob` based download strategy with `Access-Control-Expose-Headers` and `inline` disposition for maximum reliability across browsers.
- **Export Summary Modal**: Added a confirmation modal before exporting, showing the total count of items to be processed based on current filters.
- **CSV Improvements**: Added UTF-8 BOM to CSV exports to ensure perfect compatibility with Korean characters in Microsoft Excel.
- **DB Search Performance**: Optimized the PostgreSQL backend by enabling the `pg_trgm` extension and creating GIN trigram indexes on key searchable fields.

---

# Release Notes - v1.3.5 (Old)

## 🐳 UPX Compression Optimization
- **Faster Builds**: Changed UPX compression level from `--best` to `-1` to significantly reduce build and compression times, optimizing the Docker and local development workflows.

---

# Release Notes - v1.3.4 (Old)

## 🧹 Auto-Archive Older Tasks (7 Days)
- **Automatic Task Management**: Tasks older than 7 days are now automatically moved to the "Archive" section to keep your active dashboard clean.
- **NeonDB Sleep Optimization**: The archival logic uses a "piggybacking" strategy, running only when the database is already awake during message scans.
- **Rate-Limited Maintenance**: Archival updates are throttled to run at most once every 6 hours, ensuring minimal impact on performance.

---

# Release Notes - v1.3.3 (Old)

## 🪵 Leveled Logging & Dynamic Config
- **Leveled Logging System**: Introduced `debugf`, `infof`, `warnf`, and `errorf` helper functions to categorize application logs.
- **Dynamic LOG_LEVEL**: Added support for the `LOG_LEVEL` environment variable (set via `.env` or system env).
- **Reduced Verbosity**: By default (`INFO` level), verbose debug and trace logs are now hidden, resulting in much cleaner production logs on the VPS.
- **Library Integration**: Successfully mapped the internal `whatsmeow` WhatsApp library logs to the application's global `LOG_LEVEL` setting.

---

# Release Notes - v1.3.2 (Old)

## 🐳 Docker Build Optimization
- **BuildKit Cache Mounts**: Added `--mount=type=cache` to `Dockerfile` for both Go module downloads and build outputs. This allows Docker to reuse the build cache during incremental builds, resulting in significant speed improvements.
- **Fast Re-builds**: Second-time builds in Docker now benefit from partial compilation and cached dependencies, mimicking local machine performance.

---

# Release Notes - v1.3.1 (Old)

## ⚡ Build Optimization & Clean-up
- **CGO Disabled (Static Builds)**: Removed `sqlite3` (CGO) dependency in favor of a faster, fully static Go build process.
- **Linker Performance**: Optimized build scripts (`Makefile`, `Dockerfile`, `.deploy`) to ensure `CGO_ENABLED=0` for significantly faster incremental builds on dev machines.
- **Standardized Build**: Updated all build procedures to consistently use the optimized flags across local and container environments.

---

# Release Notes - v1.3.0 (Old)

## 📧 What's New: Gmail Integration & Better UX

### 🚀 Gmail as a New Message Source
- **Automated Email Scanning**: Connect your company Gmail to automatically scan for task-related emails.
- **AI-Powered Inbox Analysis**: Uses Gemini Pro to extract tasks from plain-text email bodies, identifying requesters and due dates just like Slack and WhatsApp messages.
- **Secure Token Management**: Each user can securely connect their Gmail account via OAuth 2.0 (`gmail.readonly` scope). Tokens are stored safely in the database and auto-refreshed as needed.

### 🎨 Improved Connection UX
- **Icon-Driven Connectivity**: You can now connect your WhatsApp or Gmail account by simply clicking their respective icons in the header when they are in the "OFF" state.
- **Visual Feedback**: Dashboard header icons now use standard colors for "Connected" and grayscale for "Disconnected," with interactive status tooltips.

---

# Release Notes - v1.2.0 (Stable)

## 🚀 What's New

### 👥 Multi-User & Multi-Session Support
- **Google Login (OAuth 2.0)**: Individual accounts now have their own secure sessions.
- **WhatsApp Multi-Client**: Multiple users can connect their own WhatsApp accounts simultaneously. The system uses session persistence to keep you logged in.
- **Improved UI Layout**: Switched to a modern tab-based design ('My Tasks' vs 'Other Tasks') for better task management.

### ⚡ Performance & Reliability
- **Neon DB Optimization**: Implemented in-memory caching and optimized connection pooling to allow the database to sleep during idle periods, saving resources.
- **Log Rotation**: Integrated `lumberjack.v2` for daily log rotation and automatic 7-day retention to prevent disk overflow on the VPS.
- **Dockerized Deployment**: Fully containerized setup for easy scaling and deployment on any VPS.

### 🛠 Fixes & Refinements
- **UI Polish**: Updated to a sleek dark theme with glassmorphism effects.
- **Soft Delete**: Tasks are now archived rather than permanently deleted, allowing for recovery and export.
- **Error Handling**: Improved backend error reporting with transparent JSON responses.

---
*Note: The LINE Messenger integration was attempted but rolled back due to API/Thrift client compatibility issues on the current infrastructure. Future attempts may be considered with updated libraries.*
