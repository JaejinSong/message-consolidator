-- name: GetAchievementCount :one
SELECT COUNT(*) FROM achievements;

-- name: DeleteAllAchievements :exec
DELETE FROM achievements;

-- name: SeedAchievements :exec
INSERT INTO achievements (name, description, icon, criteria_type, criteria_value, target_value, xp_reward) VALUES 
('첫 걸음', '첫 번째 업무를 완료했습니다.', '🌱', 'total_tasks', 1, 1, 10),
('모닝 스타', '오전 9시 이전에 첫 번째 업무를 완료했습니다.', '🌅', 'early_bird', 1, 1, 50),
('불끄기 (Fire Extinguisher)', '긴급(Emergency) 태스크를 완료했습니다.', '🧯', 'emergency_tasks', 1, 1, 50),
('Task Master', '하루 10개 이상의 작업 완료', '🏆', 'daily_total', 10, 10, 50),
('스트릭 스타터', '3일 연속으로 업무를 완료했습니다.', '🔥', 'streak', 3, 3, 50),
('끈기 끝판왕', '7일 연속으로 업무를 완료했습니다.', '👑', 'streak', 7, 7, 50),
('태스크 마스터 I', '누적 10개의 업무를 완료했습니다.', '🏅', 'total_tasks', 10, 10, 50),
('태스크 마스터 II', '누적 50개의 업무를 완료했습니다.', '🎖️', 'total_tasks', 50, 50, 100),
('태스크 마스터 III', '누적 100개의 업무를 완료했습니다!', '🏆', 'total_tasks', 100, 100, 200),
('꾸준함의 시작', '레벨 5에 도달했습니다.', '⭐', 'level', 5, 5, 100);
