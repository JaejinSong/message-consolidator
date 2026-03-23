package store

import (
	"database/sql"
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
