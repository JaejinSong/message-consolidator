-- name: GetTelegramSession :one
SELECT session_data FROM telegram_sessions WHERE email = ?1;

-- name: UpsertTelegramSession :exec
INSERT INTO telegram_sessions (email, session_data, updated_at)
VALUES (?1, ?2, CURRENT_TIMESTAMP)
ON CONFLICT(email) DO UPDATE SET session_data = ?2, updated_at = CURRENT_TIMESTAMP;

-- name: DeleteTelegramSession :exec
DELETE FROM telegram_sessions WHERE email = ?1;
