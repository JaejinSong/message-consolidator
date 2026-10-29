-- name: CreateGrant :exec
INSERT OR IGNORE INTO task_grants (grantor_user_id, grantee_user_id)
VALUES (?1, ?2);

-- name: DeleteGrant :exec
DELETE FROM task_grants
WHERE grantor_user_id = ?1 AND grantee_user_id = ?2;

-- name: GetGrant :one
SELECT id, grantor_user_id, grantee_user_id, created_at FROM task_grants
WHERE grantor_user_id = ?1 AND grantee_user_id = ?2;
