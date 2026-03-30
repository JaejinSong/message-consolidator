# Release Notes - v2.4.2 (2026-03-30 01:24 UTC)

- [NEW] Smart Identity Matching: Our new "Ambiguity Safeguard" ensures that whether a colleague messages you on Slack, Gmail, or Discord, we know exactly who's who—no more ghost accounts or duplicate contacts!
- [IMPROVED] Smarter AI Brain: We've fixed a pesky bug where the AI summary would sometimes cut off mid-sentence. Your weekly insights are now complete, polished, and more insightful than ever.
- [IMPROVED] Dashboard Accuracy: The "All Clear" celebration on your dashboard is now more precise, giving you that satisfying "job well done" feeling only when every task is truly handled.
- [STABILITY] Behind-the-Scenes Polish: We've cleaned up our internal engine and added new safety tests to keep the app running smoother and more reliably during your busiest hours.
- [UI] Insights Visualization & Report Layout: Resolved layout interference between the Stats and Reports tabs and improved markdown table/link readability in dark mode.
- [SYS] Multi-Language Support: Improved our internal build system to ensure all users receive the latest updates in their preferred language without delay.

---

# Release Notes - v2.4.1 (2026-03-29 12:43 UTC)

- [NEW] Weekly AI Insights: Get a smart summary of your team's week! Our new AI generates weekly reports and a "Relationship Graph" to show how work flows between colleagues.
- [NEW] Enhanced System Monitoring: We've integrated professional-grade monitoring to ensure the app stays lightning-fast and ultra-reliable 24/7.
- [IMPROVED] Snappier Performance: By upgrading our database to cutting-edge technology, your messages and tasks now load faster than ever.
- [IMPROVED] Mobile Comfort: We’ve polished the mobile interface, fixing tight margins so you can read and manage tasks comfortably on the go.
- [FIX] Assignee Visibility: Fixed a bug where some task owners were hidden or shown as "undefined." Now everyone's contributions are clearly labeled.
- [UI] Clearer Categories: Renamed certain archive folders to more intuitive names like "Canceled Tasks" to help you stay organized.

---

# Release Notes - v2.4.0 (2026-03-29 12:15 UTC)

## 🛡️ Smarter Identity Resolution & Secure Data Management

- **[NEW] Automatic Identity Sanitization**: Even if names come in differently from email or Slack, the system now automatically links them to the correct contact information, keeping your data clean.
- **[NEW] Ambiguity Protection**: To prevent errors when colleagues share the same name, the system now identifies these cases and flags them as `(Ambiguous)` instead of making incorrect guesses.
- **[IMPROVED] Enhanced Search Capabilities**: The system now understands nicknames and full names alongside IDs, ensuring tasks are correctly assigned no matter how a colleague is mentioned.
- **[FIX] Optimized Performance**: Improved internal matching logic for faster response times and perfectly accurate data visualization in your dashboard.
- **[DOCS] Documentation Sync**: All release notes have been normalized to `v2.4.0`, clearing up previous versioning discrepancies.

---

# Release Notes - v2.3.14 (2026-03-29 09:45 UTC)

- **[FEAT] Advanced Identity Resolution & Relationship Mapping**: Implemented a multi-stage identity resolution engine that prioritizes email identifiers while preserving user-defined aliases. This significantly improves the accuracy of communication network visualizations.
- **[FEAT] Relationship Visualization Graph**: Introduced a dynamic network map in AI Weekly Reports to visualize team interactions and identify communication silos.
- **[OPTIMIZE] Archive Triage Logic**: Enhanced the archive sorting algorithm to prioritize completed tasks and standardized naming for cancelled items to improve dashboard clarity.
- **[PERF] Global Edge Database Migration**: Relocated the primary database to edge infrastructure, reducing latency and improving responsiveness for users worldwide.
- **[I18N] Cross-Project Document Localization**: Standardized multi-language document management for release notes and automated report summaries across all supported locales.

---

# Release Notes - v2.3.13 (2026-03-28 16:30 UTC)

- **[FEAT] AI Weekly Report Localization**: Added full localization support (KR, EN, ID) for AI-generated insights and trend analysis reports.
- **[UI] Production-ready Insights**: Removed beta placeholders from the Insights tab, enabling full access to real-time productivity metrics.

---

# Release Notes - v2.3.12 (2026-03-28 15:30 UTC)

- **[PERF] Large-scale Task Processing**: Optimized backend reconciliation logic to handle projects with >10,000 active tasks without UI degradation.
- **[FIX] Missing Attribute Sanitization**: Resolved an issue where AI-extracted metadata fields (sender/receiver) could occasionally appear as "null" in the dashboard.

---

# Release Notes - v2.3.11 (2026-03-28 07:10 UTC)

- **[FEAT] Archive Triage Prioritization**: Reversed the sorting order in the Archive tab to show the most recently completed or cancelled tasks at the top.
- **[UI] Active Status Re-labeling**: Renamed the "Deleted" status to **"Cancelled"** to better reflect the intentionality of task management.
- **[FIX] Archive Filter Precision**: Corrected a logical error in the archive view that occasionally mixed completed and ongoing tasks when filtering by channel.

---

# Release Notes - v2.3.10 (2026-03-27 11:00 UTC)

- **[UI] Redesigned "Empty" States**: Implemented high-fidelity "All Clear" illustrations and motivational messaging for the main dashboard and archive.
- **[FEAT] Real-time Status Icons**: Unified the dashboard status icons with the new design system tokens for consistent visual identity.

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
- **[SYS] Hardened Request Validation**: Added defensive middleware to validate incoming webhooks from Slack and WhatsApp.

---

# Release Notes - v2.3.6 (2026-03-26 09:15 UTC)

- **[OPTIMIZE] Dashboard Auto-Refresh**: Refined the frontend polling logic to ensure that task completion counts match the actual message states immediately.

---

# Release Notes - v2.3.5 (2026-03-25 10:05 UTC)

- **[NEW] Activity Heatmap Visualization**: Added a GitHub-style activity map to the Insights tab, allowing users to track their message consolidation productivity over time.
- **[PERF] Static Asset Compression**: Enabled Gzip/Brotli compression at the Nginx layer for all JS and CSS assets.

---

# Release Notes - v2.3.4 (2026-03-24 07:15 UTC)

- **[FIX] Gmail Assignee Extraction**: Improved AI prompt engineering to handle complex email threads.
- **[STABILITY] Automatic Schema Migration**: Implemented a startup check that automatically updates database views and indexes.

---

# Release Notes - v2.3.3 (2026-03-24 03:30 UTC)

- **[REFACTOR] SQL View Abstraction**: Introduced `v_messages` and `v_users` views to standardize data retrieval.
- **[SYS] Mandatory Pre-deployment Testing**: Updated `deploy.sh` to require successful completion of both Go backend and JS frontend tests.

---

# Release Notes - v2.3.2 (2026-03-24 02:05 UTC)
- **[REFACTOR] Utility Standardization**: Replaced custom date/time utilities with native `Intl` and `Date` APIs for better performance and maintainability.
- **[REFACTOR] Logic Consolidation**: Unified message post-processing logic and error handling across the backend.
- **[STABILITY] Enhanced Error Response**: Standardized error responses and added explicit handling for cancelled requests (HTTP 499).
- **[PERF] Optimized Data Operations**: Improved database row scanning and slice operations using Go 1.21+ `slices` package.
