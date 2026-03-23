package store

import (
	"fmt"
	"strings"
	"time"
)

func UpdateUserGamification(email string, points, streak, level, xp, dailyGoal int, lastCompleted *time.Time, streakFreezes int) error {
	_, err := db.Exec(`
		UPDATE users 
		SET points = $1, streak = $2, level = $3, xp = $4, daily_goal = $5, last_completed_at = $6, streak_freezes = $7 
		WHERE email = $8`,
		points, streak, level, xp, dailyGoal, lastCompleted, streakFreezes, email)

	if err == nil {
		metadataMu.Lock()
		if u, ok := userCache[email]; ok {
			u.Points = points
			u.Streak = streak
			u.Level = level
			u.XP = xp
			u.DailyGoal = dailyGoal
			u.LastCompletedAt = lastCompleted
			u.StreakFreezes = streakFreezes
		}
		metadataMu.Unlock()
	}
	return err
}

func GetAchievements() ([]Achievement, error) {
	rows, err := db.Query("SELECT id, name, COALESCE(description, ''), COALESCE(icon, ''), criteria_type, criteria_value, target_value, xp_reward FROM achievements")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var achievements = []Achievement{}
	for rows.Next() {
		var a Achievement
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Icon, &a.CriteriaType, &a.CriteriaValue, &a.TargetValue, &a.XPReward); err != nil {
			return nil, err
		}
		achievements = append(achievements, a)
	}
	return achievements, nil
}

func GetUserAchievements(userID int) ([]UserAchievement, error) {
	query := fmt.Sprintf("SELECT user_id, achievement_id, unlocked_at FROM user_achievements WHERE user_id = %d", userID)
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ua = []UserAchievement{}
	for rows.Next() {
		var a UserAchievement
		if err := rows.Scan(&a.UserID, &a.AchievementID, &a.UnlockedAt); err != nil {
			return nil, err
		}
		ua = append(ua, a)
	}
	return ua, nil
}

func UnlockAchievement(userID, achievementID int) error {
	_, err := db.Exec("INSERT INTO user_achievements (user_id, achievement_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", userID, achievementID)
	return err
}

// CheckAndUnlockAchievements evaluates if the user meets any new achievement criteria and unlocks them.
func CheckAndUnlockAchievements(user User) ([]Achievement, error) {
	achievements, err := GetAchievements()
	if err != nil {
		return nil, err
	}

	userAchievements, err := GetUserAchievements(user.ID)
	if err != nil {
		return nil, err
	}

	unlockedMap := make(map[int]bool)
	for _, ua := range userAchievements {
		unlockedMap[ua.AchievementID] = true
	}

	var totalCompleted int
	query := fmt.Sprintf("SELECT COUNT(*)::int FROM messages WHERE user_email = '%s' AND done = true", strings.ReplaceAll(user.Email, "'", "''"))
	_ = db.QueryRow(query).Scan(&totalCompleted)

	var newlyUnlocked []Achievement
	for _, ach := range achievements {
		if unlockedMap[ach.ID] {
			continue // Already unlocked
		}

		unlocked := false
		if ach.CriteriaType == "total_tasks" && totalCompleted >= ach.CriteriaValue {
			unlocked = true
		} else if ach.CriteriaType == "level" && user.Level >= ach.CriteriaValue {
			unlocked = true
		}

		if unlocked && UnlockAchievement(user.ID, ach.ID) == nil {
			newlyUnlocked = append(newlyUnlocked, ach)
		}
	}

	return newlyUnlocked, nil
}
