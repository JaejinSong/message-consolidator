package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
)

// GetAllUsers retrieves all users and their aliases from the database to ensure data consistency.
func GetAllUsers(ctx context.Context) ([]User, error) {
	conn := GetDB()
	queries := db.New(conn)
	rows, err := queries.GetAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	metadataMu.Lock()
	defer metadataMu.Unlock()
	
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, fromDBUser(row))
		userCache[row.Email.String] = &users[len(users)-1]
	}

	// Why: Fetch and populate all aliases in one batch to prevent N+1 queries.
	aliasRows, err := queries.GetAllUserAliases(ctx)
	if err != nil {
		return users, nil // Return users even if alias fetch fails
	}

	mapAliasesToUsers(users, aliasRows)
	return users, nil
}

func mapAliasesToUsers(users []User, aliasRows []db.GetAllUserAliasesRow) {
	for _, row := range aliasRows {
		for i := range users {
			if int64(users[i].ID) == row.UserID {
				users[i].Aliases = append(users[i].Aliases, row.AliasName)
				if cached, ok := userCache[users[i].Email]; ok {
					cached.Aliases = users[i].Aliases
				}
				break
			}
		}
	}
}

// GetOrCreateUser fetches a user from the cache or database by email.
// If the user doesn't exist, it creates a new record. It also updates the user's name and picture if new values are provided.
func GetOrCreateUser(ctx context.Context, email, name, picture string) (*User, error) {
	metadataMu.Lock()
	if u, ok := userCache[email]; ok {
		//Why: Trigger a database update if a new name or picture is provided and differs from the cached data.
		if (name != "" && u.Name != name) || (picture != "" && u.Picture != picture) {
			metadataMu.Unlock()
			return updateAndCacheUser(ctx, email, name, picture)
		}
		metadataMu.Unlock()
		return u, nil
	}
	metadataMu.Unlock()

	return updateAndCacheUser(ctx, email, name, picture)
}

// updateAndCacheUser handles the database upsert logic for a user and securely updates the in-memory cache.
func updateAndCacheUser(ctx context.Context, email, name, picture string) (*User, error) {
	var u User
	err := WithDBRetry("GetOrCreateUser", func() error {
		//Why: Use Atomic Upsert to handle concurrent registrations and partial data updates (name/picture) in a single call.
		conn := GetDB()
		row, errQuery := db.New(conn).UpsertUser(ctx, db.UpsertUserParams{
			Email:   sql.NullString{String: email, Valid: email != ""},
			Name:    sql.NullString{String: name, Valid: name != ""},
			Picture: sql.NullString{String: picture, Valid: picture != ""},
		})
		if errQuery != nil {
			return errQuery
		}

		u = fromDBUser(row)
		return nil
	})

	if err != nil {
		return nil, err
	}

	metadataMu.Lock()
	userCache[email] = &u
	metadataMu.Unlock()

	if name != "" {
		_ = AddUserAlias(ctx, u.ID, name)
	}

	return &u, nil
}

// CreateUser inserts a new user record into the database with just an email and name.
func CreateUser(ctx context.Context, email, name string) error {
	conn := GetDB()
	queries := db.New(conn)
	_, err := queries.UpsertUser(ctx, db.UpsertUserParams{
		Email: sql.NullString{String: email, Valid: email != ""},
		Name:  sql.NullString{String: name, Valid: name != ""},
	})
	return err
}

// UpdateUserNamePicture modifies the display name and profile picture of an existing user.
func UpdateUserNamePicture(ctx context.Context, email, name, picture string) error {
	return db.New(GetDB()).UpdateUserDetails(ctx, db.UpdateUserDetailsParams{
		Email:   sql.NullString{String: email, Valid: true},
		Name:    sql.NullString{String: name, Valid: name != ""},
		Picture: sql.NullString{String: picture, Valid: picture != ""},
	})
}

// UpdateUserWAJID updates the WhatsApp JID (identifier) associated with the user.
func UpdateUserWAJID(ctx context.Context, email, wajid string) error {
	return db.New(GetDB()).UpdateUserDetails(ctx, db.UpdateUserDetailsParams{
		Email: sql.NullString{String: email, Valid: true},
		WaJid: sql.NullString{String: wajid, Valid: wajid != ""},
	})
}

// UpdateUserSlackID updates the Slack ID associated with the user.
func UpdateUserSlackID(ctx context.Context, email, slackID string) error {
	return db.New(GetDB()).UpdateUserDetails(ctx, db.UpdateUserDetailsParams{
		Email:   sql.NullString{String: email, Valid: true},
		SlackID: sql.NullString{String: slackID, Valid: slackID != ""},
	})
}

func GetUserAliasesByEmail(ctx context.Context, email string) ([]string, error) {
	metadataMu.RLock()
	if u, ok := userCache[email]; ok && len(u.Aliases) > 0 {
		metadataMu.RUnlock()
		return u.Aliases, nil
	}
	metadataMu.RUnlock()

	aliases, err := db.New(GetDB()).GetUserAliasesByEmail(ctx, sql.NullString{String: email, Valid: true})
	if err != nil {
		return nil, err
	}
	
	// Why: Update cache with loaded aliases for subsequent requests.
	metadataMu.Lock()
	updateUserCacheAliases(email, aliases)
	metadataMu.Unlock()
	
	return aliases, err
}

func updateUserCacheAliases(email string, aliases []string) {
	if u, ok := userCache[email]; ok {
		u.Aliases = aliases
	}
}

func GetUserName(ctx context.Context, email string) (string, error) {
	return db.New(GetDB()).GetUserByEmailSimple(ctx, sql.NullString{String: email, Valid: email != ""})
}



func fromDBUser(row db.User) User {
	return User{
		ID:        int(row.ID),
		Email:     row.Email.String,
		Name:      row.Name.String,
		SlackID:   row.SlackID.String,
		WAJID:     row.WaJid.String,
		Picture:   row.Picture.String,
		CreatedAt: row.CreatedAt.Time,
	}
}
