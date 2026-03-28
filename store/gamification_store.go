package store

import (
	"database/sql"
	"message-consolidator/logger"
	"sync"
	"time"
)

var (
	achievementCache []Achievement
	achCacheMu       sync.RWMutex
)

// UpdateUserGamification updates the user's gamification stats in the database and synchronizes the memory cache.
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

// GetAchievements retrieves all available achievements. It utilizes a double-checked locking pattern to safely and efficiently manage the cache.
func GetAchievements() ([]Achievement, error) {
	achCacheMu.RLock()
	if len(achievementCache) > 0 {
		defer achCacheMu.RUnlock()
		return achievementCache, nil
	}
	achCacheMu.RUnlock()

	achCacheMu.Lock()
	defer achCacheMu.Unlock()

	//Why: Performs a double-check of the cache after acquiring the write lock to prevent race conditions.
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

// GetUserAchievements retrieves the list of achievements unlocked by the user.
// It also performs a retroactive check to unlock any achievements the user might have met the criteria for since their last check.
func GetUserAchievements(userID int) ([]UserAchievement, error) {
	//Why: Retrieves user information and triggers a retroactive achievement check before returning the unlocked list.
	var u User
	var lastCompletedAt, createdAt DBTime
	var slackID, waJID sql.NullString
	err := db.QueryRow(SQL.GetUserByID, userID).Scan(
		&u.ID, &u.Email, &u.Name, &slackID, &waJID, &u.Picture,
		&u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal,
		&lastCompletedAt, &createdAt, &u.StreakFreezes,
	)
	if err == nil {
		u.SlackID = slackID.String
		u.WAJID = waJID.String
		if lastCompletedAt.Valid && !lastCompletedAt.Time.IsZero() {
			u.LastCompletedAt = &lastCompletedAt.Time
		}
		u.CreatedAt = createdAt.Time
		_, _ = CheckAndUnlockAchievements(u)
	} else if err != sql.ErrNoRows {
		logger.Warnf("[GAMIFICATION] Failed to get user for retroactive check (ID: %d): %v", userID, err)
	}

	return getUnlockedAchievementsFromDB(userID)
}

// getUnlockedAchievementsFromDB retrieves the list of unlocked achievements purely from the database (internal use only).
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

// UnlockAchievement persists a newly unlocked achievement for a user in the database.
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

	//Why: Invokes the internal retrieval function directly to prevent infinite loops or circular dependencies.
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
			continue //Why: Skips achievements that have already been unlocked to avoid redundant processing.
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
