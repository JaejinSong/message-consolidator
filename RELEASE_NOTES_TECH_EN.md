# Release Notes (Tech) - v2.4.6 (2026-04-19 15:33 UTC)



---

# Release Notes (Tech) - v2.4.5 (2026-04-17 06:32 UTC)

- [STABILITY] Replace file-based test DB with unique in-memory SQLite to resolve concurrency issues
- [OPTIMIZE] Refactor database initialization, connection pooling, and schema migration for Turso and SQLite
- [FIX] Prevent JSON marshaling errors by handling empty metadata
- [FIX] Correct archive logic and message de-duplication mechanism

---

# Release Notes (Tech) - v2.4.4 (2026-04-17 04:28 UTC)

- [REFACTOR] Remove time-based filtering from message cache and archive queries
- [REFACTOR] Migrate message processing status tracking to scan_metadata table
- [FIX] Disable whitespace trimming in ExtractJSONBlock regex to prevent data loss
- [FEAT] Implement strict report structure with mandatory headers and optimized JSON extraction
- [FEAT] Integrate WhatsApp PushName resolution for contact mapping
- [FIX] Deploy backend-driven task classification with alias synchronization
- [FEAT] Enhance assignee identification using aliases and include source context in summaries

---

# Release Notes (Tech) - v2.4.3 (2026-04-17 02:02 UTC)

- [FEAT] Upgrade AI engine to gemini-3-flash-preview and increase report cutoff size to 16,000
- [FEAT] Implement task deduplication using Jaro-Winkler similarity and prevent concurrent processing
- [FEAT] Add channel/status filters to reports with i18n support and enhanced assignee identification
- [UI] Integrate pulse animations and refine report rendering logic
- [REFACTOR] Consolidate database operations for users, contacts, and messages into unified Upsert/Update interfaces
- [REFACTOR] Centralize database error logging and implement cache lookup validation helpers
- [SYS] Enhance ArchiveOldTasks with row-count logging and implement early returns in completion pipelines

---

# Release Notes (Tech) - v2.4.2 (2026-04-15 02:30 UTC)

- [STABILITY] Enhance database connection resilience and implement comprehensive error logging
- [REFACTOR] Implement hierarchical task extraction with subtask support and unified assignee mapping
- [FEAT] Integrate AI-driven asynchronous task state transitions and completion processing
- [FEAT] Deploy asynchronous report generation engine with status polling mechanism

---

# Release Notes (Tech) - v2.4.1 (2026-04-14 05:27 UTC)

- [REFACTOR] Deprecate gamification and achievement modules to streamline core logic and database schema
- [FEAT] Introduce AI-driven noise filtering and marketing detection for Gmail processing
- [FEAT] Implement per-room locking service to prevent race conditions and improve observability
- [REFACTOR] Standardize message classification and assignee normalization across Slack and WhatsApp scanners
- [REFACTOR] Centralize API error handling and context cancellation for improved system stability
- [FEAT] Add text similarity service and refined system prompts for more accurate task resolution
- [SYS] Integrate WaitGroup in scanner pipeline for graceful asynchronous task management
- [PERF] Transition to in-memory testing infrastructure and optimize state synchronization
- [FEAT] Track and expose noise filtering statistics in token usage metrics

---

