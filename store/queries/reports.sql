-- name: CreateReportsTable
CREATE TABLE IF NOT EXISTS reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email TEXT NOT NULL,
    start_date TEXT NOT NULL,
    end_date TEXT NOT NULL,
    summary TEXT NOT NULL,
    visualization TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: UpsertReport
INSERT OR REPLACE INTO reports (user_email, start_date, end_date, summary, visualization, created_at)
VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP);

-- name: GetReport
SELECT id, user_email, start_date, end_date, summary, visualization, created_at
FROM reports
WHERE user_email = ? AND start_date = ? AND end_date = ?;

-- name: GetMessagesForReport
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id
FROM messages
WHERE user_email = ? 
  AND (created_at >= ? OR assigned_at >= ?)
  AND is_deleted = 0
ORDER BY created_at DESC;

-- name: DeleteOldReports
DELETE FROM reports WHERE created_at < datetime('now', '-30 days');
