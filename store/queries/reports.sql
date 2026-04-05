-- name: CreateReportsTable
DROP TABLE IF EXISTS reports;
CREATE TABLE reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email TEXT NOT NULL,
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL,
    visualization TEXT NOT NULL,
    is_truncated INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateReportTranslationsTable
CREATE TABLE IF NOT EXISTS report_translations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_id INTEGER NOT NULL,
    language_code TEXT NOT NULL,
    language_deprecated TEXT,
    summary TEXT NOT NULL,
    FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE
);

-- name: CreateReportTranslationsIndex
CREATE UNIQUE INDEX IF NOT EXISTS idx_report_translations_id_lang_code ON report_translations (report_id, language_code);

-- name: InsertReport
INSERT INTO reports (user_email, start_date, end_date, visualization, is_truncated, created_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP);

-- name: InsertReportTranslation :exec
INSERT INTO report_translations (report_id, language_code, summary)
VALUES (?, ?, ?)
ON CONFLICT(report_id, language_code) DO UPDATE SET summary = EXCLUDED.summary;

-- name: GetReport
SELECT r.id, r.user_email, r.start_date, r.end_date, r.visualization, r.is_truncated, r.created_at, rt.summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.user_email = ? AND r.start_date = ? AND r.end_date = ?;

-- name: GetReportByDate
SELECT r.id, r.user_email, r.start_date, r.end_date, r.visualization, r.is_truncated, r.created_at, rt.summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.user_email = ? AND r.start_date = ?;

-- name: GetMessagesForReport
SELECT 
    m.id, m.user_email, m.source, m.room, 
    COALESCE(t.translated_text, m.task) AS task, 
    m.requester, m.assignee, m.assigned_at, m.link, m.source_ts, m.original_text, m.done, m.is_deleted, m.created_at, m.completed_at, m.category, m.deadline, m.thread_id,
    m.assignee_reason, m.replied_to_id, m.is_context_query, m.constraints, m.metadata, m.requester_canonical, m.assignee_canonical
FROM v_messages m
LEFT JOIN task_translations t ON m.id = t.message_id AND t.language_code = 'en'
WHERE m.user_email = ? 
  AND (m.created_at >= ? OR m.assigned_at >= ?)
ORDER BY m.created_at DESC;

-- name: DeleteOldReports
DELETE FROM reports WHERE created_at < datetime('now', '-30 days');

-- name: ListReports
SELECT r.id, r.start_date, r.end_date, r.created_at, r.is_truncated, rt.summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.user_email = ?
ORDER BY r.created_at DESC;

-- name: GetReportList
SELECT id, start_date, end_date, created_at
FROM reports
WHERE user_email = ?
ORDER BY created_at DESC;

-- name: GetReportByID
SELECT r.id, r.user_email, r.start_date, r.end_date, r.visualization, r.is_truncated, r.created_at, rt.summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.id = ? AND r.user_email = ?;

-- name: GetReportTranslations
SELECT language_code, summary
FROM report_translations
WHERE report_id = ?;

-- name: DeleteReport
DELETE FROM reports WHERE id = ? AND user_email = ?;
