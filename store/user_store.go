package store

import (
	"database/sql"
)

// GetAllUsers retrieves all users directly from the database to ensure data consistency and discover any newly added users.
// It also synchronizes the in-memory cache with the latest data.
func GetAllUsers() ([]User, error) {
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
		var slackID, waJID sql.NullString
		var lastCompletedAt, createdAt DBTime
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &slackID, &waJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastCompletedAt, &createdAt, &u.StreakFreezes); err != nil {
			return nil, err
		}
		u.SlackID = slackID.String
		u.WAJID = waJID.String
		if lastCompletedAt.Valid && !lastCompletedAt.Time.IsZero() {
			u.LastCompletedAt = &lastCompletedAt.Time
		}
		u.CreatedAt = createdAt.Time

		// Set a fallback for DailyGoal to prevent division by zero or logical errors in legacy data.
		if u.DailyGoal <= 0 {
			u.DailyGoal = 5
		}

		// Synchronize the retrieved user data with the in-memory cache.
		userCache[u.Email] = &u
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// GetOrCreateUser fetches a user from the cache or database by email.
// If the user doesn't exist, it creates a new record. It also updates the user's name and picture if new values are provided.
func GetOrCreateUser(email, name, picture string) (*User, error) {
	metadataMu.Lock()
	if u, ok := userCache[email]; ok {
		// If a new name or picture is provided and differs from the cached data, trigger a database update.
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

// updateAndCacheUser handles the database upsert logic for a user and securely updates the in-memory cache.
func updateAndCacheUser(email, name, picture string) (*User, error) {
	var u User
	err := WithDBRetry("GetOrCreateUser", func() error {
		var slackID, waJID sql.NullString
		var lastCompletedAt, createdAt DBTime
		errQuery := db.QueryRow(SQL.GetUserByEmail, email).Scan(&u.ID, &u.Email, &u.Name, &slackID, &waJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastCompletedAt, &createdAt, &u.StreakFreezes)
		if errQuery == sql.ErrNoRows {
			errQuery = db.QueryRow(SQL.CreateUserReturningAll, email, name, picture, 5).Scan(&u.ID, &u.Email, &u.Name, &slackID, &waJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastCompletedAt, &createdAt, &u.StreakFreezes)
		}
		if errQuery != nil {
			return errQuery
		}
		u.SlackID = slackID.String
		u.WAJID = waJID.String
		if lastCompletedAt.Valid && !lastCompletedAt.Time.IsZero() {
			u.LastCompletedAt = &lastCompletedAt.Time
		}
		u.CreatedAt = createdAt.Time

		// Flag for update if a valid, new name or picture is provided.
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

// CreateUser inserts a new user record into the database with just an email and name.
func CreateUser(email, name string) error {
	_, err := db.Exec(SQL.CreateUser, email, name)
	return err
}

// UpdateUserNamePicture modifies the display name and profile picture of an existing user.
func UpdateUserNamePicture(email, name, picture string) error {
	_, err := db.Exec(SQL.UpdateUserNamePicture, name, picture, email)
	return err
}

// UpdateUserWAJID updates the WhatsApp JID (identifier) associated with the user.
func UpdateUserWAJID(email, wajid string) error {
	_, err := db.Exec(SQL.UpdateUserWAJID, wajid, email)
	return err
}

// UpdateUserSlackID updates the Slack ID associated with the user.
func UpdateUserSlackID(email, slackID string) error {
	_, err := db.Exec(SQL.UpdateUserSlackID, slackID, email)
	return err
}
