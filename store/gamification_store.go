package store

import (
	"message-consolidator/logger"
	"sync"
	"time"
)

var (
	achievementCache []Achievement
	achCacheMu       sync.RWMutex
)

func UpdateUserGamification(email string, points, streak, level, xp, dailyGoal int, lastCompleted *time.Time, streakFreezes int) error {
	_, err := db.Exec(SQL.UpdateUserGamification,
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
	achCacheMu.RLock()
	if len(achievementCache) > 0 {
		defer achCacheMu.RUnlock()
		return achievementCache, nil
	}
	achCacheMu.RUnlock()

	achCacheMu.Lock()
	defer achCacheMu.Unlock()

	// Double check after lock
	if len(achievementCache) > 0 {
		return achievementCache, nil
	}

	rows, err := db.Query(SQL.GetAchievements)
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

	achievementCache = achievements
	logger.Infof("[Store] Cached %d achievements", len(achievementCache))
	return achievements, nil
}

func GetUserAchievements(userID int) ([]UserAchievement, error) {
	// 1. 소급 적용을 위해 사용자 정보를 가져와 업적 체크를 먼저 수행
	var u User
	// SQL.GetUserByID (SELECT * FROM v_users WHERE id = ?)
	err := db.QueryRow(SQL.GetUserByID, userID).Scan(
		&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture,
		&u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal,
		&u.LastCompletedAt, &u.CreatedAt, &u.StreakFreezes,
	)
	if err == nil {
		_, _ = CheckAndUnlockAchievements(u)
	}

	return getUnlockedAchievementsFromDB(userID)
}

// getUnlockedAchievementsFromDB는 순수하게 DB에서 해제된 업적 목록만 조회 (내부용)
func getUnlockedAchievementsFromDB(userID int) ([]UserAchievement, error) {
	rows, err := db.Query(SQL.GetUserAchievements, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ua = []UserAchievement{}
	for rows.Next() {
		var a UserAchievement
		var unlockedAt DBTime
		if err := rows.Scan(&a.UserID, &a.AchievementID, &unlockedAt); err != nil {
			return nil, err
		}
		a.UnlockedAt = unlockedAt.Time
		ua = append(ua, a)
	}
	return ua, nil
}

func UnlockAchievement(userID, achievementID int) error {
	_, err := db.Exec(SQL.UnlockAchievement, userID, achievementID)
	return err
}

// CheckAndUnlockAchievements evaluates if the user meets any new achievement criteria and unlocks them.
func CheckAndUnlockAchievements(user User) ([]Achievement, error) {
	achievements, err := GetAchievements()
	if err != nil {
		return nil, err
	}

	// 순환 참조 방지를 위해 내부 함수 직접 호출
	userAchievements, err := getUnlockedAchievementsFromDB(user.ID)
	if err != nil {
		return nil, err
	}

	unlockedMap := make(map[int]bool)
	for _, ua := range userAchievements {
		unlockedMap[ua.AchievementID] = true
	}

	var totalCompleted, earlyBirdCount, maxDailyCount int
	_ = db.QueryRow(SQL.GetTotalCompleted, user.Email).Scan(&totalCompleted)
	_ = db.QueryRow(SQL.GetEarlyBirdCompleted, user.Email).Scan(&earlyBirdCount)
	_ = db.QueryRow(SQL.GetMaxDailyCompleted, user.Email).Scan(&maxDailyCount)

	var newlyUnlocked []Achievement
	for _, ach := range achievements {
		if unlockedMap[ach.ID] {
			continue // Already unlocked
		}

		unlocked := false
		switch ach.CriteriaType {
		case "total_tasks":
			unlocked = totalCompleted >= ach.CriteriaValue
		case "level":
			unlocked = user.Level >= ach.CriteriaValue
		case "early_bird":
			unlocked = earlyBirdCount >= ach.CriteriaValue
		case "daily_total":
			unlocked = maxDailyCount >= ach.CriteriaValue
		case "streak":
			unlocked = user.Streak >= ach.CriteriaValue
		}

		if unlocked && UnlockAchievement(user.ID, ach.ID) == nil {
			newlyUnlocked = append(newlyUnlocked, ach)
		}
	}

	return newlyUnlocked, nil
}
