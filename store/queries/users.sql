-- name: GetAllUsers :many
SELECT id, COALESCE(email, '') as email, name, slack_id, wa_jid, picture, COALESCE(points, 0) as points, COALESCE(streak, 0) as streak, COALESCE(level, 0) as level, COALESCE(xp, 0) as xp, COALESCE(daily_goal, 0) as daily_goal, last_completed_at, created_at, COALESCE(streak_freezes, 0) as streak_freezes FROM v_users;

-- name: GetUserByEmail :one
SELECT id, COALESCE(email, '') as email, name, slack_id, wa_jid, picture, COALESCE(points, 0) as points, COALESCE(streak, 0) as streak, COALESCE(level, 0) as level, COALESCE(xp, 0) as xp, COALESCE(daily_goal, 0) as daily_goal, last_completed_at, created_at, COALESCE(streak_freezes, 0) as streak_freezes FROM v_users WHERE email = ?1;

-- name: GetUserByID :one
SELECT id, COALESCE(email, '') as email, name, slack_id, wa_jid, picture, COALESCE(points, 0) as points, COALESCE(streak, 0) as streak, COALESCE(level, 0) as level, COALESCE(xp, 0) as xp, COALESCE(daily_goal, 0) as daily_goal, last_completed_at, created_at, COALESCE(streak_freezes, 0) as streak_freezes FROM v_users WHERE id = CAST(?1 AS INTEGER);

-- name: CreateUser :one
INSERT INTO users (email, name, picture) 
VALUES (?1, ?2, ?3) 
ON CONFLICT(email) DO UPDATE SET 
    name = EXCLUDED.name, 
    picture = EXCLUDED.picture
RETURNING id, COALESCE(email, '') as email, COALESCE(name, '') as name, COALESCE(slack_id, '') as slack_id, COALESCE(wa_jid, '') as wa_jid, COALESCE(picture, '') as picture, COALESCE(points, 0) as points, COALESCE(streak, 0) as streak, COALESCE(level, 0) as level, COALESCE(xp, 0) as xp, COALESCE(daily_goal, 0) as daily_goal, last_completed_at, created_at, COALESCE(streak_freezes, 0) as streak_freezes;

-- name: CreateUserReturningAll :one
INSERT INTO users (email, name, picture, daily_goal) 
VALUES (?1, ?2, ?3, CAST(?4 AS INTEGER)) 
ON CONFLICT(email) DO UPDATE SET 
    name = COALESCE(NULLIF(EXCLUDED.name, ''), users.name),
    picture = COALESCE(NULLIF(EXCLUDED.picture, ''), users.picture),
    daily_goal = CASE WHEN EXCLUDED.daily_goal > 0 THEN EXCLUDED.daily_goal ELSE users.daily_goal END
RETURNING id, COALESCE(email, '') as email, COALESCE(name, '') as name, COALESCE(slack_id, '') as slack_id, COALESCE(wa_jid, '') as wa_jid, COALESCE(picture, '') as picture, COALESCE(points, 0) as points, COALESCE(streak, 0) as streak, COALESCE(level, 0) as level, COALESCE(xp, 0) as xp, COALESCE(daily_goal, 0) as daily_goal, last_completed_at, created_at, COALESCE(streak_freezes, 0) as streak_freezes;

-- name: GetUserByEmailSimple :one
SELECT COALESCE(name, '') as name FROM users WHERE email = ?1;

-- name: UpdateUserNamePicture :exec
UPDATE users SET name = ?1, picture = ?2 WHERE email = ?3;

-- name: UpdateUserWAJID :exec
UPDATE users SET wa_jid = ?1 WHERE email = ?2;

-- name: UpdateUserSlackID :exec
UPDATE users SET slack_id = ?1 WHERE email = ?2;

-- name: MigrateUsersAddPoints :exec
ALTER TABLE users ADD COLUMN points INTEGER DEFAULT 0;

-- name: MigrateUsersAddStreak :exec
ALTER TABLE users ADD COLUMN streak INTEGER DEFAULT 0;

-- name: MigrateUsersAddLevel :exec
ALTER TABLE users ADD COLUMN level INTEGER DEFAULT 1;

-- name: MigrateUsersAddXP :exec
ALTER TABLE users ADD COLUMN xp INTEGER DEFAULT 10;

-- name: MigrateUsersAddDailyGoal :exec
ALTER TABLE users ADD COLUMN daily_goal INTEGER DEFAULT 5;

-- name: MigrateUsersAddLastCompletedAt :exec
ALTER TABLE users ADD COLUMN last_completed_at DATETIME;

-- name: MigrateUsersAddStreakFreezes :exec
ALTER TABLE users ADD COLUMN streak_freezes INTEGER DEFAULT 0;

-- name: GetUserAliasesByEmail :many
SELECT alias_name FROM user_aliases a
JOIN users u ON a.user_id = u.id
WHERE u.email = ?1;
