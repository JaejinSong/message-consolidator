-- name: CreateGrant :exec
INSERT OR IGNORE INTO task_grants (grantor_user_id, grantee_user_id)
VALUES (?1, ?2);

-- name: DeleteGrant :exec
DELETE FROM task_grants
WHERE grantor_user_id = ?1 AND grantee_user_id = ?2;

-- name: GetGrant :one
SELECT id, grantor_user_id, grantee_user_id, created_at FROM task_grants
WHERE grantor_user_id = ?1 AND grantee_user_id = ?2;

-- name: ListGranteesOf :many
SELECT u.id, u.email, u.name, u.slack_id, u.wa_jid, u.tg_user_id, u.picture, u.is_admin, u.created_at
FROM users u
JOIN task_grants tg ON u.id = tg.grantee_user_id
WHERE tg.grantor_user_id = ?1;

-- name: ListGrantorsFor :many
SELECT u.id, u.email, u.name, u.slack_id, u.wa_jid, u.tg_user_id, u.picture, u.is_admin, u.created_at
FROM users u
JOIN task_grants tg ON u.id = tg.grantor_user_id
WHERE tg.grantee_user_id = ?1;
