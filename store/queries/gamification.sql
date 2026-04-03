-- name: UpdateUserGamification :exec
UPDATE users 
SET points = ?, streak = ?, level = ?, xp = ?, daily_goal = ?, last_completed_at = ?, streak_freezes = ? 
WHERE email = ?;

-- name: GetAchievements :many
SELECT id, name, COALESCE(description, ''), COALESCE(icon, ''), criteria_type, criteria_value, target_value, xp_reward 
FROM achievements;

-- name: GetUserAchievements :many
SELECT user_id, achievement_id, unlocked_at 
FROM user_achievements 
WHERE user_id = ?;

-- name: UnlockAchievement :exec
INSERT INTO user_achievements (user_id, achievement_id, unlocked_at) 
VALUES (?, ?, CURRENT_TIMESTAMP) 
ON CONFLICT (user_id, achievement_id) 
DO UPDATE SET unlocked_at = EXCLUDED.unlocked_at;
