-- name: GetAllUsers :many
SELECT * FROM v_users;

-- name: GetUserByEmail :one
SELECT * FROM v_users WHERE email = ?;

-- name: GetUserByID :one
SELECT * FROM v_users WHERE id = ?;

-- name: CreateUser :one
INSERT INTO users (email, name, picture) 
VALUES (?, ?, ?) 
ON CONFLICT(email) DO UPDATE SET 
    name = EXCLUDED.name, 
    picture = EXCLUDED.picture
RETURNING id, email, COALESCE(name, '') as name, COALESCE(slack_id, '') as slack_id, COALESCE(wa_jid, '') as wa_jid, COALESCE(picture, '') as picture, points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes;

-- name: CreateUserReturningAll :one
INSERT INTO users (email, name, picture, daily_goal) 
VALUES (?, ?, ?, ?) 
ON CONFLICT(email) DO UPDATE SET 
    name = EXCLUDED.name, 
    picture = EXCLUDED.picture,
    daily_goal = EXCLUDED.daily_goal
RETURNING id, email, COALESCE(name, '') as name, COALESCE(slack_id, '') as slack_id, COALESCE(wa_jid, '') as wa_jid, COALESCE(picture, '') as picture, points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes;

-- name: GetUserByEmailSimple :one
SELECT name FROM users WHERE email = ?;

-- name: UpdateUserNamePicture :exec
UPDATE users SET name = ?, picture = ? WHERE email = ?;

-- name: UpdateUserWAJID :exec
UPDATE users SET wa_jid = ? WHERE email = ?;

-- name: UpdateUserSlackID :exec
UPDATE users SET slack_id = ? WHERE email = ?;

-- name: GetUserAliases :many
SELECT alias_name FROM user_aliases a
JOIN users u ON a.user_id = u.id
WHERE u.email = ?;
