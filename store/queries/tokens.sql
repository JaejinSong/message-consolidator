CREATE TABLE IF NOT EXISTS token_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email VARCHAR(255) NOT NULL,
    date DATE NOT NULL DEFAULT (date('now')),
    step TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    report_id INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    total_tokens INT DEFAULT 0,
    call_count INT DEFAULT 0,
    filtered_count INT DEFAULT 0,
    UNIQUE(user_email, date, step, model, source, report_id)
);

-- name: UpsertTokenUsage :exec
INSERT INTO token_usage (user_email, date, step, model, source, report_id, prompt_tokens, completion_tokens, total_tokens, call_count, filtered_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (user_email, date, step, model, source, report_id)
DO UPDATE SET
    prompt_tokens = token_usage.prompt_tokens + EXCLUDED.prompt_tokens,
    completion_tokens = token_usage.completion_tokens + EXCLUDED.completion_tokens,
    total_tokens = token_usage.total_tokens + EXCLUDED.total_tokens,
    call_count = token_usage.call_count + EXCLUDED.call_count,
    filtered_count = token_usage.filtered_count + EXCLUDED.filtered_count;

-- name: GetReportTokenUsage :one
-- Cost dashboard: prompt/completion/calls aggregated for a single report. Sums across the
-- 3 report-bound steps (ReportSummary/ReportVizData/TranslateReport) plus any future buckets.
SELECT COALESCE(SUM(prompt_tokens), 0)     AS prompt_tokens,
       COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
       COALESCE(SUM(call_count), 0)        AS call_count
FROM token_usage
WHERE report_id = ?;

-- name: GetDailyTokenUsage :one
SELECT COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(filtered_count), 0)
FROM token_usage
WHERE user_email = ? AND date = ?;

-- name: GetMonthlyTokenUsage :one
SELECT COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(filtered_count), 0)
FROM token_usage
WHERE user_email = ? AND date >= ? AND date < ?;

-- name: GetTokenUsageByStep :many
-- Dashboard: per-step breakdown over a date range (inclusive start, exclusive end).
SELECT step,
       COALESCE(SUM(prompt_tokens), 0)     AS prompt_tokens,
       COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
       COALESCE(SUM(call_count), 0)        AS call_count
FROM token_usage
WHERE user_email = ? AND date >= ? AND date < ?
GROUP BY step
ORDER BY prompt_tokens DESC;

-- name: GetTokenUsageByModel :many
SELECT model,
       COALESCE(SUM(prompt_tokens), 0)     AS prompt_tokens,
       COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
       COALESCE(SUM(call_count), 0)        AS call_count
FROM token_usage
WHERE user_email = ? AND date >= ? AND date < ?
GROUP BY model
ORDER BY prompt_tokens DESC;

-- name: GetTokenUsageBySource :many
SELECT source,
       COALESCE(SUM(prompt_tokens), 0)     AS prompt_tokens,
       COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
       COALESCE(SUM(call_count), 0)        AS call_count
FROM token_usage
WHERE user_email = ? AND date >= ? AND date < ?
GROUP BY source
ORDER BY prompt_tokens DESC;

-- name: UpsertGmailToken :exec
INSERT INTO gmail_tokens (user_email, token_json, updated_at)
VALUES (?, ?, DATETIME('now'))
ON CONFLICT (user_email) 
DO UPDATE SET token_json = EXCLUDED.token_json, updated_at = DATETIME('now');

-- name: GetGmailToken :one
SELECT token_json FROM gmail_tokens WHERE user_email = ?;

-- name: DeleteGmailToken :exec
DELETE FROM gmail_tokens WHERE user_email = ?;
