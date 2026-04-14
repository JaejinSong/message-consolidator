-- name: GetAllUsers :many
SELECT 
    id, 
    COALESCE(email, '') as email, 
    COALESCE(name, '') as name, 
    COALESCE(slack_id, '') as slack_id, 
    COALESCE(wa_jid, '') as wa_jid, 
    COALESCE(picture, '') as picture, 
    created_at 
FROM users;

-- name: GetUserByEmail :one
SELECT 
    id, 
    COALESCE(email, '') as email, 
    COALESCE(name, '') as name, 
    COALESCE(slack_id, '') as slack_id, 
    COALESCE(wa_jid, '') as wa_jid, 
    COALESCE(picture, '') as picture, 
    created_at 
FROM users 
WHERE email = ?1;

-- name: GetUserByID :one
SELECT 
    id, 
    COALESCE(email, '') as email, 
    COALESCE(name, '') as name, 
    COALESCE(slack_id, '') as slack_id, 
    COALESCE(wa_jid, '') as wa_jid, 
    COALESCE(picture, '') as picture, 
    created_at 
FROM users 
WHERE id = CAST(?1 AS INTEGER);

-- name: CreateUser :one
INSERT INTO users (email, name, picture) 
VALUES (?1, ?2, ?3) 
ON CONFLICT(email) DO UPDATE SET 
    name = EXCLUDED.name, 
    picture = EXCLUDED.picture
RETURNING 
    id, 
    COALESCE(email, '') as email, 
    COALESCE(name, '') as name, 
    COALESCE(slack_id, '') as slack_id, 
    COALESCE(wa_jid, '') as wa_jid, 
    COALESCE(picture, '') as picture, 
    created_at;

-- name: CreateUserReturningAll :one
INSERT INTO users (email, name, picture) 
VALUES (?1, ?2, ?3) 
ON CONFLICT(email) DO UPDATE SET 
    name = COALESCE(NULLIF(EXCLUDED.name, ''), users.name),
    picture = COALESCE(NULLIF(EXCLUDED.picture, ''), users.picture)
RETURNING 
    id, 
    COALESCE(email, '') as email, 
    COALESCE(name, '') as name, 
    COALESCE(slack_id, '') as slack_id, 
    COALESCE(wa_jid, '') as wa_jid, 
    COALESCE(picture, '') as picture, 
    created_at;

-- name: GetUserByEmailSimple :one
SELECT COALESCE(name, '') as name FROM users WHERE email = ?1;

-- name: UpdateUserNamePicture :exec
UPDATE users SET name = ?1, picture = ?2 WHERE email = ?3;

-- name: UpdateUserWAJID :exec
UPDATE users SET wa_jid = ?1 WHERE email = ?2;

-- name: UpdateUserSlackID :exec
UPDATE users SET slack_id = ?1 WHERE email = ?2;

-- name: GetUserAliasesByEmail :many
SELECT alias_name FROM user_aliases a
JOIN users u ON a.user_id = u.id
WHERE u.email = ?1;


