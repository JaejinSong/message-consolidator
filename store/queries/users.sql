-- name: GetAllUsers :many
SELECT * FROM v_users;

-- name: GetUserByEmail :one
SELECT * FROM v_users WHERE email = ?;

-- name: CreateUser :one
INSERT INTO users (email, name, picture) 
VALUES (?, ?, ?) 
RETURNING id, email, COALESCE(name, '') as name, COALESCE(slack_id, '') as slack_id, COALESCE(wa_jid, '') as wa_jid, COALESCE(picture, '') as picture, points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes;

-- name: CreateUserReturningAll :one
INSERT INTO users (email, name, picture, daily_goal) 
VALUES (?, ?, ?, ?) 
RETURNING id, email, COALESCE(name, '') as name, COALESCE(slack_id, '') as slack_id, COALESCE(wa_jid, '') as wa_jid, COALESCE(picture, '') as picture, points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes;

-- name: GetUserByEmailSimple :one
SELECT name FROM users WHERE email = ?;

-- name: UpdateUserNamePicture :exec
UPDATE users SET name = ?, picture = ? WHERE email = ?;

-- name: UpdateUserWAJID :exec
UPDATE users SET wa_jid = ? WHERE email = ?;

-- name: UpdateUserSlackID :exec
UPDATE users SET slack_id = ? WHERE email = ?;
