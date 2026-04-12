-- name: GetAchievementsCount :one
SELECT COUNT(*) FROM achievements;

-- name: DeleteAllAchievements :exec
DELETE FROM achievements;

-- name: SeedAchievements :exec
INSERT INTO achievements (name, description, icon, criteria_type, criteria_value, target_value, xp_reward) VALUES 
('Morning Star', 'Completed task before 9 AM', 'morning_star', 'early_bird', 1, 1, 50),
('First Step', 'Completed first task', 'task', 'total_tasks', 1, 1, 10),
('Steady Effort', 'Completed 5 tasks', 'task', 'total_tasks', 5, 5, 20),
('Experienced User', 'Completed 10 tasks', 'task', 'total_tasks', 10, 10, 50),
('Level Up', 'Reached level 2', 'level', 'level', 2, 2, 30),
('3 Day Streak', 'Completed tasks for 3 consecutive days', 'streak', 'streak', 3, 3, 20),
('Weekly Passion', 'Completed tasks for 7 consecutive days', 'streak', 'streak', 7, 7, 100),
('Perfectionist', 'Completed 50 tasks', 'task', 'total_tasks', 50, 50, 200),
('Master', 'Completed 100 tasks', 'task', 'total_tasks', 100, 100, 500);
