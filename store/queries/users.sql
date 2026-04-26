-- name: GetAllUsers :many
SELECT id, email, name, slack_id, wa_jid, tg_user_id, picture, is_admin, created_at FROM users;

-- name: GetUserByEmail :one
SELECT id, email, name, slack_id, wa_jid, tg_user_id, picture, is_admin, created_at FROM users WHERE email = ?1;

-- name: GetUserByID :one
SELECT id, email, name, slack_id, wa_jid, tg_user_id, picture, is_admin, created_at FROM users WHERE id = CAST(?1 AS INTEGER);

-- name: UpsertUser :one
INSERT INTO users (email, name, picture)
VALUES (?1, ?2, ?3)
ON CONFLICT(email) DO UPDATE SET
    name = COALESCE(NULLIF(EXCLUDED.name, ''), users.name),
    picture = COALESCE(NULLIF(EXCLUDED.picture, ''), users.picture)
RETURNING id, email, name, slack_id, wa_jid, tg_user_id, picture, is_admin, created_at;

-- name: GetUserByEmailSimple :one
SELECT COALESCE(name, '') as name FROM users WHERE email = ?1;

-- name: UpdateUserDetails :exec
UPDATE users
SET
    name = COALESCE(sqlc.narg('name'), name),
    picture = COALESCE(sqlc.narg('picture'), picture),
    wa_jid = COALESCE(sqlc.narg('wa_jid'), wa_jid),
    slack_id = COALESCE(sqlc.narg('slack_id'), slack_id),
    tg_user_id = COALESCE(sqlc.narg('tg_user_id'), tg_user_id)
WHERE email = ?1;

-- name: SetUserAdmin :exec
UPDATE users SET is_admin = ?2 WHERE email = ?1;

-- name: ListAdminUsers :many
SELECT id, email, name, slack_id, wa_jid, tg_user_id, picture, is_admin, created_at FROM users WHERE is_admin = 1;

-- name: GetUserAliasesByEmail :many
SELECT alias_name FROM user_aliases a
JOIN users u ON a.user_id = u.id
WHERE u.email = ?1;
