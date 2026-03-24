-- name: UpsertTenantAlias :exec
INSERT INTO tenant_aliases (user_email, original_name, primary_name) 
VALUES (?, ?, ?) 
ON CONFLICT (user_email, original_name) 
DO UPDATE SET primary_name = EXCLUDED.primary_name;

-- name: DeleteTenantAlias :exec
DELETE FROM tenant_aliases WHERE user_email = ? AND original_name = ?;

-- name: GetUserAliases :many
SELECT alias_name FROM user_aliases WHERE user_id = ?;

-- name: CreateUserAlias :exec
INSERT INTO user_aliases (user_id, alias_name) 
VALUES (?, ?) 
ON CONFLICT (user_id, alias_name) 
DO NOTHING;

-- name: DeleteUserAlias :exec
DELETE FROM user_aliases WHERE user_id = ? AND alias_name = ?;

-- name: GetAllTenantAliases :many
SELECT user_email, original_name, primary_name FROM tenant_aliases;

-- name: GetAllUserAliases :many
SELECT user_id, alias_name FROM user_aliases;
