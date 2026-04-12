-- name: UpdateUserGamification :exec
UPDATE users 
SET points = ?1, streak = ?2, level = ?3, xp = ?4, daily_goal = ?5, last_completed_at = ?6, streak_freezes = ?7 
WHERE email = ?8;

-- name: GetAchievements :many
SELECT id, name, COALESCE(description, '') as description, COALESCE(icon, '') as icon, 
       COALESCE(criteria_type, '') as criteria_type, COALESCE(criteria_value, 0) as criteria_value, 
       COALESCE(target_value, 0) as target_value, COALESCE(xp_reward, 0) as xp_reward 
FROM achievements;

-- name: GetUserAchievements :many
SELECT 
    COALESCE(user_id, 0) as user_id, 
    COALESCE(achievement_id, 0) as achievement_id, 
    unlocked_at 
FROM user_achievements 
WHERE user_id = CAST(?1 AS INTEGER);

-- name: UnlockAchievement :exec
INSERT INTO user_achievements (user_id, achievement_id, unlocked_at) 
VALUES (CAST(?1 AS INTEGER), CAST(?2 AS INTEGER), CURRENT_TIMESTAMP) 
ON CONFLICT (user_id, achievement_id) 
DO UPDATE SET unlocked_at = EXCLUDED.unlocked_at;
