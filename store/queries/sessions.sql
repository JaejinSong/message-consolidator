-- name: CreateSession :exec
INSERT INTO sessions (token, email, expires_at, created_at)
VALUES (?, ?, ?, DATETIME('now'));

-- name: GetSession :one
SELECT token, email, expires_at, created_at FROM sessions
WHERE token = ? AND expires_at > DATETIME('now');

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= DATETIME('now');
