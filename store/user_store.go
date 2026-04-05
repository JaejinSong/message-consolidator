package store

import (
	"database/sql"
)

// GetAllUsers retrieves all users and their aliases from the database to ensure data consistency.
func GetAllUsers() ([]User, error) {
	rows, err := db.Query(SQL.GetAllUsers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metadataMu.Lock()
	defer metadataMu.Unlock()
	
	users, err := scanAndCacheUsers(rows)
	if err != nil {
		return nil, err
	}

	// Why: Fetch and populate all aliases in one batch to prevent N+1 queries.
	aliasRows, err := db.Query(SQL.GetAllUserAliases)
	if err != nil {
		return users, nil // Return users even if alias fetch fails
	}
	defer aliasRows.Close()

	for aliasRows.Next() {
		var userID int
		var aliasName string
		if err := aliasRows.Scan(&userID, &aliasName); err == nil {
			for i := range users {
				if users[i].ID == userID {
					users[i].Aliases = append(users[i].Aliases, aliasName)
					userCache[users[i].Email].Aliases = users[i].Aliases
					break
				}
			}
		}
	}

	return users, nil
}

func scanAndCacheUsers(rows *sql.Rows) ([]User, error) {
	var users []User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		userCache[u.Email] = &u
		users = append(users, u)
	}
	return users, rows.Err()
}

func scanUserRow(rows *sql.Rows) (User, error) {
	var u User
	var slackID, waJID sql.NullString
	var lastComp, created DBTime
	err := rows.Scan(&u.ID, &u.Email, &u.Name, &slackID, &waJID, &u.Picture, &u.Points, &u.Streak, &u.Level, &u.XP, &u.DailyGoal, &lastComp, &created, &u.StreakFreezes)
	if err != nil {
		return u, err
	}
	u.SlackID, u.WAJID = slackID.String, waJID.String
	if lastComp.Valid && !lastComp.Time.IsZero() {
		u.LastCompletedAt = &lastComp.Time
	}
	u.CreatedAt = created.Time
	if u.DailyGoal <= 0 {
		u.DailyGoal = 5
	}
	return u, nil
}

// GetOrCreateUser fetches a user from the cache or database by email.
// If the user doesn't exist, it creates a new record. It also updates the user's name and picture if new values are provided.
func GetOrCreateUser(email, name, picture string) (*User, error) {
	metadataMu.Lock()
	if u, ok := userCache[email]; ok {
		//Why: Trigger a database update if a new name or picture is provided and differs from the cached data.
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

		//Why: Flags for update if a valid, new name or picture is provided.
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

func GetUserAliasesByEmail(email string) ([]string, error) {
	metadataMu.RLock()
	if u, ok := userCache[email]; ok && len(u.Aliases) > 0 {
		metadataMu.RUnlock()
		return u.Aliases, nil
	}
	metadataMu.RUnlock()

	rows, err := db.Query(SQL.GetUserAliases, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var aliases []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err == nil {
			aliases = append(aliases, a)
		}
	}
	
	// Why: Update cache with loaded aliases for subsequent requests.
	metadataMu.Lock()
	if u, ok := userCache[email]; ok {
		u.Aliases = aliases
	}
	metadataMu.Unlock()
	
	return aliases, rows.Err()
}

func GetUserName(email string) (string, error) {
	var name string
	err := db.QueryRow(SQL.GetUserByEmailSimple, email).Scan(&name)
	return name, err
}
