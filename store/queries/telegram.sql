-- name: GetTelegramSession :one
SELECT session_data FROM telegram_sessions WHERE email = ?1;

-- name: UpsertTelegramSession :exec
INSERT INTO telegram_sessions (email, session_data, updated_at)
VALUES (?1, ?2, CURRENT_TIMESTAMP)
ON CONFLICT(email) DO UPDATE SET session_data = ?2, updated_at = CURRENT_TIMESTAMP;

-- name: DeleteTelegramSession :exec
DELETE FROM telegram_sessions WHERE email = ?1;

-- name: GetTelegramCredentials :one
SELECT app_id, app_hash FROM telegram_credentials WHERE email = ?1;

-- name: UpsertTelegramCredentials :exec
INSERT INTO telegram_credentials (email, app_id, app_hash, updated_at)
VALUES (?1, ?2, ?3, CURRENT_TIMESTAMP)
ON CONFLICT(email) DO UPDATE SET app_id = ?2, app_hash = ?3, updated_at = CURRENT_TIMESTAMP;

-- name: DeleteTelegramCredentials :exec
DELETE FROM telegram_credentials WHERE email = ?1;
