-- name: CreateUserAlias :exec
INSERT INTO user_aliases (user_id, alias_name) 
VALUES (?, ?) 
ON CONFLICT (user_id, alias_name) 
DO NOTHING;

-- name: GetAllUserAliases :many
SELECT user_id, alias_name FROM user_aliases;
