package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"message-consolidator/logger"
	"sync"
	"time"
)

var (
	achievementCache []Achievement
	achCacheMu       sync.RWMutex
)

// UpdateUserGamification updates the user's gamification stats in the database and synchronizes the memory cache.
func UpdateUserGamification(ctx context.Context, email string, points, streak, level, xp, dailyGoal int, lastCompleted *time.Time, streakFreezes int) error {
	queries := db.New(GetDB())
	var lastComp sql.NullTime
	if lastCompleted != nil {
		lastComp = sql.NullTime{Time: *lastCompleted, Valid: true}
	}

	err := queries.UpdateUserGamification(ctx, db.UpdateUserGamificationParams{
		Points:          sql.NullInt64{Int64: int64(points), Valid: true},
		Streak:          sql.NullInt64{Int64: int64(streak), Valid: true},
		Level:           sql.NullInt64{Int64: int64(level), Valid: true},
		Xp:              sql.NullInt64{Int64: int64(xp), Valid: true},
		DailyGoal:       sql.NullInt64{Int64: int64(dailyGoal), Valid: true},
		LastCompletedAt: lastComp,
		StreakFreezes:   sql.NullInt64{Int64: int64(streakFreezes), Valid: true},
		Email:           sql.NullString{String: email, Valid: true},
	})

	if err == nil {
		updateUserCache(email, points, streak, level, xp, dailyGoal, lastCompleted, streakFreezes)
	}
	return err
}

func updateUserCache(email string, points, streak, level, xp, dailyGoal int, lastCompleted *time.Time, streakFreezes int) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	if u, ok := userCache[email]; ok {
		u.Points = points
		u.Streak = streak
		u.Level = level
		u.XP = xp
		u.DailyGoal = dailyGoal
		u.LastCompletedAt = lastCompleted
		u.StreakFreezes = streakFreezes
	}
}

// GetAchievements retrieves all available achievements.
func GetAchievements(ctx context.Context) ([]Achievement, error) {
	achCacheMu.RLock()
	if len(achievementCache) > 0 {
		defer achCacheMu.RUnlock()
		return achievementCache, nil
	}
	achCacheMu.RUnlock()

	achCacheMu.Lock()
	defer achCacheMu.Unlock()

	if len(achievementCache) > 0 {
		return achievementCache, nil
	}

	queries := db.New(GetDB())
	achs, err := queries.GetAchievements(ctx)
	if err != nil {
		return nil, err
	}

	var achievements []Achievement
	for _, a := range achs {
		achievements = append(achievements, Achievement{
			ID:            int(a.ID),
			Name:          a.Name,
			Description:   a.Description,
			Icon:          a.Icon,
			CriteriaType:  a.CriteriaType,
			CriteriaValue: int(a.CriteriaValue),
			TargetValue:   int(a.TargetValue),
			XPReward:      int(a.XpReward),
		})
	}

	achievementCache = achievements
	logger.Infof("[Store] Cached %d achievements", len(achievementCache))
	return achievements, nil
}

// GetUserAchievements retrieves the list of achievements unlocked by the user.
func GetUserAchievements(ctx context.Context, userID int) ([]UserAchievement, error) {
	queries := db.New(GetDB())
	u, err := queries.GetUserByID(ctx, int64(userID))
	if err == nil {
		userObj := User{
			ID:            int(u.ID),
			Email:         u.Email,
			Name:          u.Name,
			SlackID:       u.SlackID,
			WAJID:         u.WaJid,
			Picture:       u.Picture,
			Points:        int(u.Points),
			Streak:        int(u.Streak),
			Level:         int(u.Level),
			XP:            int(u.Xp),
			DailyGoal:     int(u.DailyGoal),
			StreakFreezes: int(u.StreakFreezes),
			CreatedAt:     u.CreatedAt.Time,
		}
		if u.LastCompletedAt.Valid {
			userObj.LastCompletedAt = &u.LastCompletedAt.Time
		}
		_, _ = CheckAndUnlockAchievements(ctx, userObj)
	} else if err != sql.ErrNoRows {
		logger.Warnf("[GAMIFICATION] Failed to get user for retroactive check (ID: %d): %v", userID, err)
	}

	return getUnlockedAchievementsFromDB(ctx, int(userID))
}

func getUnlockedAchievementsFromDB(ctx context.Context, userID int) ([]UserAchievement, error) {
	queries := db.New(GetDB())
	rows, err := queries.GetUserAchievements(ctx, int64(userID))
	if err != nil {
		return nil, err
	}

	var ua []UserAchievement
	for _, a := range rows {
		ua = append(ua, UserAchievement{
			UserID:        int(a.UserID),
			AchievementID: int(a.AchievementID),
			UnlockedAt:    a.UnlockedAt.Time,
		})
	}
	return ua, nil
}

// UnlockAchievement persists a newly unlocked achievement for a user in the database.
func UnlockAchievement(ctx context.Context, userID, achievementID int) error {
	queries := db.New(GetDB())
	return queries.UnlockAchievement(ctx, db.UnlockAchievementParams{
		Column1: int64(userID),
		Column2: int64(achievementID),
	})
}

// CheckAndUnlockAchievements evaluates if the user meets any new achievement criteria and unlocks them.
func CheckAndUnlockAchievements(ctx context.Context, user User) ([]Achievement, error) {
	achievements, err := GetAchievements(ctx)
	if err != nil {
		return nil, err
	}

	userAchievements, err := getUnlockedAchievementsFromDB(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	unlockedMap := make(map[int]bool)
	for _, ua := range userAchievements {
		unlockedMap[ua.AchievementID] = true
	}

	queries := db.New(GetDB())
	totalCompleted, _ := queries.GetTotalCompleted(ctx, user.Email)
	earlyBirdCount, _ := queries.GetEarlyBirdCompleted(ctx, user.Email)
	maxDC, _ := queries.GetMaxDailyCompleted(ctx, user.Email)
	
	// SQLite MAX() returns int64 for count-based aggregates in sqlc
	var maxDaily int64
	if v, ok := maxDC.(int64); ok {
		maxDaily = v
	}

	var newlyUnlocked []Achievement
	for _, ach := range achievements {
		if unlockedMap[ach.ID] {
			continue
		}

		if isAchievementMet(ach, user, int64(totalCompleted), int64(earlyBirdCount), maxDaily) {
			if UnlockAchievement(ctx, int(user.ID), int(ach.ID)) == nil {
				newlyUnlocked = append(newlyUnlocked, ach)
			}
		}
	}

	return newlyUnlocked, nil
}

func isAchievementMet(ach Achievement, user User, total, early, daily int64) bool {
	switch ach.CriteriaType {
	case "total_tasks":
		return total >= int64(ach.CriteriaValue)
	case "level":
		return int64(user.Level) >= int64(ach.CriteriaValue)
	case "early_bird":
		return early >= int64(ach.CriteriaValue)
	case "daily_total":
		return daily >= int64(ach.CriteriaValue)
	case "streak":
		return int64(user.Streak) >= int64(ach.CriteriaValue)
	default:
		return false
	}
}
