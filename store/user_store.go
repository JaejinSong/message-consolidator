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
		rows, err := db.Query("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), points, streak, level, xp, daily_goal, last_completed_at, created_at FROM users")
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		metadataMu.Lock()
		for rows.Next() {
			var u User
			if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &u.LastCompletedAt, &u.CreatedAt); err != nil {
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
		metadataMu.Unlock()
		return u, nil
	}
	metadataMu.Unlock()

	// Not in cache, fetch from DB or Create
	var u User
	err := WithDBRetry("GetOrCreateUser", func() error {
		errQuery := db.QueryRow("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), points, streak, level, xp, daily_goal, last_completed_at, created_at FROM users WHERE email = $1", email).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &u.LastCompletedAt, &u.CreatedAt)
		if errQuery == sql.ErrNoRows {
			return db.QueryRow("INSERT INTO users (email, name, picture) VALUES ($1, $2, $3) RETURNING id, email, name, COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), points, streak, level, xp, daily_goal, last_completed_at, created_at", email, name, picture).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &u.LastCompletedAt, &u.CreatedAt)
		}
		return errQuery
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

func UpdateUserGamification(email string, points, streak, level, xp, dailyGoal int, lastCompleted *time.Time) error {
	_, err := db.Exec(`
		UPDATE users 
		SET points = $1, streak = $2, level = $3, xp = $4, daily_goal = $5, last_completed_at = $6 
		WHERE email = $7`,
		points, streak, level, xp, dailyGoal, lastCompleted, email)

	if err == nil {
		metadataMu.Lock()
		if u, ok := userCache[email]; ok {
			u.Points = points
			u.Streak = streak
			u.Level = level
			u.XP = xp
			u.DailyGoal = dailyGoal
			u.LastCompletedAt = lastCompleted
		}
		metadataMu.Unlock()
	}
	return err
}

func GetAchievements() ([]Achievement, error) {
	rows, err := db.Query("SELECT id, name, description, icon, criteria_type, criteria_value FROM achievements")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var achievements []Achievement
	for rows.Next() {
		var a Achievement
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.Icon, &a.CriteriaType, &a.CriteriaValue); err != nil {
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

	var ua []UserAchievement
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

func (u *User) ProcessTaskCompletion() error {
	// 1. Give XP and Points
	newXP := u.XP + 10
	newPoints := u.Points + 5

	// 2. Handle Level Up
	newLevel := (newXP / 100) + 1

	// 3. Handle Streak
	now := time.Now()
	newStreak := u.Streak

	if u.LastCompletedAt == nil {
		newStreak = 1
	} else {
		last := *u.LastCompletedAt
		// Safe date truncation (prevents UTC epoch offset bugs)
		lastDate := time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, last.Location())
		nowDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		daysDiff := int(nowDate.Sub(lastDate).Hours() / 24)

		if daysDiff == 1 {
			newStreak++
		} else if daysDiff > 1 {
			newStreak = 1
		}
	}

	// Cache is safely updated inside UpdateUserGamification with Mutex lock
	return UpdateUserGamification(u.Email, newPoints, newStreak, newLevel, newXP, u.DailyGoal, &now)
}
