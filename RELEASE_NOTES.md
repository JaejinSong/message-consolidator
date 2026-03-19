# Release Notes - v1.7.4 (Latest)

## 🏗️ Data Model Unification & I18n Refactoring
- **RawMessage Consolidation**: Unified Slack, Gmail, and WhatsApp into a single `RawMessage` struct, removing redundant fields (`User`, `RawTS`) for a leaner data model.
- **`WAManager` Struct Implementation**: Refactored WhatsApp integration into a dedicated manager with callbacks (`OnConnected`, `FetchUserWAJID`), decoupling it from the `store` package.
- **I18n Architecture Overhaul**: Moved UI text to `locales.js` and simplified `i18n.js` to use `data-i18n` attributes, improving maintainability and reducing JS bundle overhead.
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
