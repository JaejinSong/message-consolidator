-- name: InitTokenUsageTable :exec
CREATE TABLE IF NOT EXISTS token_usage (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_email VARCHAR(255) NOT NULL,
    date DATE NOT NULL DEFAULT (date('now')),
    prompt_tokens INT DEFAULT 0,
    completion_tokens INT DEFAULT 0,
    total_tokens INT DEFAULT 0,
    UNIQUE(user_email, date)
);

-- name: UpsertTokenUsage :exec
INSERT INTO token_usage (user_email, prompt_tokens, completion_tokens, total_tokens, date)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (user_email, date)
DO UPDATE SET 
    prompt_tokens = token_usage.prompt_tokens + EXCLUDED.prompt_tokens,
    completion_tokens = token_usage.completion_tokens + EXCLUDED.completion_tokens,
    total_tokens = token_usage.total_tokens + EXCLUDED.total_tokens;

-- name: GetDailyTokenUsage :one
SELECT COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0) 
FROM token_usage 
WHERE user_email = ? AND date = ?;

-- name: GetMonthlyTokenUsage :one
SELECT COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0) 
FROM token_usage 
WHERE user_email = ? AND date >= ? AND date < ?;

-- name: UpsertGmailToken :exec
INSERT INTO gmail_tokens (user_email, token_json, updated_at)
VALUES (?, ?, DATETIME('now'))
ON CONFLICT (user_email) 
DO UPDATE SET token_json = EXCLUDED.token_json, updated_at = DATETIME('now');

-- name: GetGmailToken :one
SELECT token_json FROM gmail_tokens WHERE user_email = ?;

-- name: DeleteGmailToken :exec
DELETE FROM gmail_tokens WHERE user_email = ?;
