package store

import (
	"database/sql"
	"strings"
	"time"
)

func GetAllUsers() ([]User, error) {
	metadataMu.Lock()
	if len(userCache) == 0 {
		metadataMu.Unlock()
		// Load from DB if cache is empty
		rows, err := db.Query("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes FROM users")
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		metadataMu.Lock()
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &u.LastCompletedAt, &u.CreatedAt, &u.StreakFreezes); err != nil {
				continue
			}
			userCache[u.Email] = &u
		}
	}
	defer metadataMu.Unlock()

	var users []User
	for _, u := range userCache {
		users = append(users, *u)
	}
	return users, nil
}

func NormalizeName(tenantEmail, name string) string {
	if name == "" {
		return ""
	}

	metadataMu.RLock()
	defer metadataMu.RUnlock()

	nameLower := strings.ToLower(strings.TrimSpace(name))

	// 1. Check Tenant-specific Aliases (HIGHEST PRIORITY)
	if tenantMap, ok := tenantAliasCache[tenantEmail]; ok {
		for original, primary := range tenantMap {
			if strings.ToLower(original) == nameLower {
				return primary
			}
		}
	}

	// 2. Check Primary Names of app users
	for _, u := range userCache {
		if strings.ToLower(u.Name) == nameLower {
			return u.Name
		}
	}

	// 3. Check App User Aliases
	for userID, aliases := range aliasCache {
		for _, alias := range aliases {
			if strings.ToLower(alias) == nameLower {
				for _, u := range userCache {
					if u.ID == userID {
						return u.Name
					}
				}
			}
		}
	}

	// 4. Check Contacts Mappings (LOWEST PRIORITY)
	return NormalizeContactName(tenantEmail, name)
}

func GetTenantAliases(email string) (map[string]string, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	if m, ok := tenantAliasCache[email]; ok {
		return m, nil
	}

	// If not in cache, we could load it here, but LoadMetadata should handle it.
	// Let's return empty if not found.
	return make(map[string]string), nil
}

func AddTenantAlias(email, original, primary string) error {
	if original == "" || primary == "" {
		return nil
	}
	_, err := db.Exec("INSERT INTO tenant_aliases (user_email, original_name, primary_name) VALUES ($1, $2, $3) ON CONFLICT (user_email, original_name) DO UPDATE SET primary_name = EXCLUDED.primary_name", email, original, primary)
	if err != nil {
		return err
	}

	metadataMu.Lock()
	if _, ok := tenantAliasCache[email]; !ok {
		tenantAliasCache[email] = make(map[string]string)
	}
	tenantAliasCache[email][original] = primary
	metadataMu.Unlock()
	return nil
}

func DeleteTenantAlias(email, original string) error {
	_, err := db.Exec("DELETE FROM tenant_aliases WHERE user_email = $1 AND original_name = $2", email, original)
	if err != nil {
		return err
	}

	metadataMu.Lock()
	if _, ok := tenantAliasCache[email]; ok {
		delete(tenantAliasCache[email], original)
	}
	metadataMu.Unlock()
	return nil
}

func GetOrCreateUser(email, name, picture string) (*User, error) {
	metadataMu.Lock()
	if u, ok := userCache[email]; ok {
		// If name/picture provided and different, update them
		if (name != "" && u.Name != name) || (picture != "" && u.Picture != picture) {
			metadataMu.Unlock()
			return updateAndCacheUser(email, name, picture)
		}
		metadataMu.Unlock()
		return u, nil
	}
	metadataMu.Unlock()

	return updateAndCacheUser(email, name, picture)
}

func updateAndCacheUser(email, name, picture string) (*User, error) {
	var u User
	err := WithDBRetry("GetOrCreateUser", func() error {
		errQuery := db.QueryRow("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes FROM users WHERE email = $1", email).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &u.LastCompletedAt, &u.CreatedAt, &u.StreakFreezes)
		if errQuery == sql.ErrNoRows {
			return db.QueryRow("INSERT INTO users (email, name, picture) VALUES ($1, $2, $3) RETURNING id, email, name, COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), points, streak, level, xp, daily_goal, last_completed_at, created_at, streak_freezes", email, name, picture).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &u.LastCompletedAt, &u.CreatedAt, &u.StreakFreezes)
		}
		if errQuery != nil {
			return errQuery
		}

		// Update if name/picture is provided and different
		needsUpdate := false
		if name != "" && u.Name != name {
			u.Name = name
			needsUpdate = true
		}
		if picture != "" && u.Picture != picture {
			u.Picture = picture
			needsUpdate = true
		}

		if needsUpdate {
			_, errUpdate := db.Exec("UPDATE users SET name = $1, picture = $2 WHERE email = $3", u.Name, u.Picture, email)
			return errUpdate
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	metadataMu.Lock()
	userCache[email] = &u
	metadataMu.Unlock()

	return &u, nil
}

func UpdateUserWAJID(email, wajid string) error {
	_, err := db.Exec("UPDATE users SET wa_jid = $1 WHERE email = $2", wajid, email)
	return err
}

func UpdateUserSlackID(email, slackID string) error {
	_, err := db.Exec("UPDATE users SET slack_id = $1 WHERE email = $2", slackID, email)
	return err
}

func GetUserAliases(userID int) ([]string, error) {
	metadataMu.RLock()
	aliases, ok := aliasCache[userID]
	metadataMu.RUnlock()
	if ok {
		return aliases, nil
	}

	rows, err := db.Query("SELECT alias_name FROM user_aliases WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newAliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			continue
		}
		newAliases = append(newAliases, alias)
	}

	metadataMu.Lock()
	aliasCache[userID] = newAliases
	metadataMu.Unlock()

	return newAliases, nil
}

func AddUserAlias(userID int, alias string) error {
	if alias == "" {
		return nil
	}
	_, err := db.Exec("INSERT INTO user_aliases (user_id, alias_name) VALUES ($1, $2) ON CONFLICT (user_id, alias_name) DO NOTHING", userID, alias)
	return err
}

func DeleteUserAlias(userID int, alias string) error {
	_, err := db.Exec("DELETE FROM user_aliases WHERE user_id = $1 AND alias_name = $2", userID, alias)
	return err
}

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
	rows, err := db.Query("SELECT id, name, description, icon, criteria_type, criteria_value, target_value, xp_reward FROM achievements")
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
	rows, err := db.Query("SELECT user_id, achievement_id, unlocked_at FROM user_achievements WHERE user_id = $1", userID)
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

	// Get user's total completed tasks dynamically
	var totalCompleted int
	_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = $1 AND done = true", user.Email).Scan(&totalCompleted)

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
