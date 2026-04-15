-- name: InsertReport :one
INSERT INTO reports (user_email, start_date, end_date, visualization, status, is_truncated, created_at)
VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
RETURNING id;

-- name: InsertReportTranslation :exec
INSERT INTO report_translations (report_id, language_code, summary)
VALUES (?, ?, ?)
ON CONFLICT(report_id, language_code) DO UPDATE SET summary = EXCLUDED.summary;

-- name: GetReport :one
SELECT r.id, r.user_email, r.start_date, r.end_date, r.visualization, r.status, r.is_truncated, r.created_at, COALESCE(rt.summary, '') as summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.user_email = ? AND r.start_date = ? AND r.end_date = ?;

-- name: GetReportByDate :one
SELECT r.id, r.user_email, r.start_date, r.end_date, r.visualization, r.status, r.is_truncated, r.created_at, COALESCE(rt.summary, '') as summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.user_email = ? AND r.start_date = ? AND r.end_date = ?;

-- name: GetMessagesForReport :many
SELECT 
    m.id, m.user_email, m.source, m.room, 
    m.task, 
    m.requester, m.assignee, m.assigned_at, m.link, m.source_ts, m.pinned, m.original_text, m.done, m.is_deleted, m.created_at, m.completed_at, m.category, m.deadline, m.thread_id,
    m.assignee_reason, m.replied_to_id, m.is_context_query, m.constraints, m.metadata, m.source_channels, m.consolidated_context, m.subtasks, m.requester_canonical, m.assignee_canonical, m.requester_type, m.assignee_type
FROM v_messages m
WHERE m.user_email = ? 
  AND (m.created_at >= ? OR m.assigned_at >= ?)
ORDER BY m.created_at DESC;

-- name: DeleteOldReports :exec
DELETE FROM reports WHERE created_at < datetime('now', '-30 days');

-- name: ListReports :many
SELECT r.id, r.start_date, r.end_date, r.created_at, r.status, r.is_truncated, COALESCE(rt.summary, '') as summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.user_email = ? AND r.status = 'completed'
ORDER BY r.created_at DESC;

-- name: GetReportList :many
SELECT id, start_date, end_date, created_at, status
FROM reports
WHERE user_email = ? AND status != 'failed'
ORDER BY created_at DESC;

-- name: GetReportByID :one
SELECT r.id, r.user_email, r.start_date, r.end_date, r.visualization, r.status, r.is_truncated, r.created_at, COALESCE(rt.summary, '') as summary
FROM reports r
LEFT JOIN report_translations rt ON r.id = rt.report_id AND rt.language_code = 'en'
WHERE r.id = ? AND r.user_email = ?;

-- name: GetReportTranslations :many
SELECT language_code, summary
FROM report_translations
WHERE report_id = ?;

-- name: MigrateReportsAddIsTruncated :exec
ALTER TABLE reports ADD COLUMN is_truncated INTEGER DEFAULT 0;

-- name: MigrateReportTranslationsRenameLanguage :exec
ALTER TABLE report_translations RENAME COLUMN language TO language_deprecated;

-- name: MigrateReportTranslationsAddLanguageCode :exec
ALTER TABLE report_translations ADD COLUMN language_code TEXT;

-- name: CreateReportTranslationsIndex :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_report_translations_report_id_lang ON report_translations(report_id, language_code);

-- name: UpdateReportStatus :exec
UPDATE reports SET status = ?, visualization = ?, is_truncated = ? WHERE id = ? AND user_email = ?;

-- name: MigrateReportsAddStatus :exec
ALTER TABLE reports ADD COLUMN status TEXT DEFAULT 'completed';

-- name: DeleteReport :exec
DELETE FROM reports WHERE id = ? AND user_email = ?;
