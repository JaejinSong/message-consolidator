package store

import (
	"database/sql"
)

func GetAllUsers() ([]User, error) {
	// Always load from DB to ensure consistency and discover new users
	rows, err := db.Query(SQL.GetAllUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	metadataMu.Lock()
	defer metadataMu.Unlock()

	for rows.Next() {
		var u User
		var lastCompletedAt, createdAt DBTime
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastCompletedAt, &createdAt, &u.StreakFreezes); err != nil {
			return nil, err
		}
		if lastCompletedAt.Valid && !lastCompletedAt.Time.IsZero() {
			u.LastCompletedAt = &lastCompletedAt.Time
		}
		u.CreatedAt = createdAt.Time

		// Ensure DailyGoal is at least 1 (safety for legacy data)
		if u.DailyGoal <= 0 {
			u.DailyGoal = 5
		}

		// Sync to cache
		userCache[u.Email] = &u
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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
		var lastCompletedAt, createdAt DBTime
		errQuery := db.QueryRow(SQL.GetUserByEmail, email).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastCompletedAt, &createdAt, &u.StreakFreezes)
		if errQuery == sql.ErrNoRows {
			errQuery = db.QueryRow(SQL.CreateUserReturningAll, email, name, picture, 5).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastCompletedAt, &createdAt, &u.StreakFreezes)
		}
		if errQuery != nil {
			return errQuery
		}
		if lastCompletedAt.Valid && !lastCompletedAt.Time.IsZero() {
			u.LastCompletedAt = &lastCompletedAt.Time
		}
		u.CreatedAt = createdAt.Time

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

		if u.DailyGoal <= 0 {
			u.DailyGoal = 5
		}

		if needsUpdate {
			_, errUpdate := db.Exec(SQL.UpdateUserNamePicture, u.Name, u.Picture, email)
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

func CreateUser(email, name string) error {
	_, err := db.Exec(SQL.CreateUser, email, name)
	return err
}

func UpdateUserNamePicture(email, name, picture string) error {
	_, err := db.Exec(SQL.UpdateUserNamePicture, name, picture, email)
	return err
}

func UpdateUserWAJID(email, wajid string) error {
	_, err := db.Exec(SQL.UpdateUserWAJID, wajid, email)
	return err
}

func UpdateUserSlackID(email, slackID string) error {
	_, err := db.Exec(SQL.UpdateUserSlackID, slackID, email)
	return err
}
