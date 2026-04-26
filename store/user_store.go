package store

import (
	"context"
	"database/sql"
	"message-consolidator/db"
	"time"
)

// allUsersCacheTTL bounds staleness when an invalidation is missed. The scanner
// runs every ~59s; with a 5min ceiling we still cap DB load to ~12 GetAllUsers
// queries per hour even if no writes occur.
const allUsersCacheTTL = 5 * time.Minute

// GetAllUsers retrieves all users and their aliases.
// Memoized in-process: cross-region libsql RTT made every tick eat ~900-1800ms for
// a 1-row table. Invalidate via InvalidateAllUsersCache on any user/alias write.
func GetAllUsers(ctx context.Context) ([]User, error) {
	metadataMu.RLock()
	if allUsersCache != nil && time.Since(allUsersCachedAt) < allUsersCacheTTL {
		cached := allUsersCache
		metadataMu.RUnlock()
		return cached, nil
	}
	metadataMu.RUnlock()

	// Why: singleflight collapses concurrent misses (scanner tick + RefreshAllCaches
	// firing at boot) into a single DB round-trip.
	v, err, _ := sfGroup.Do("store.GetAllUsers", func() (any, error) {
		return loadAllUsersFromDB(ctx)
	})
	if err != nil {
		return nil, err
	}
	users, _ := v.([]User)
	return users, nil
}

func loadAllUsersFromDB(ctx context.Context) ([]User, error) {
	queries := db.New(GetDB())
	rows, err := queries.GetAllUsers(ctx)
	if err != nil {
		return nil, err
	}

	aliasRows, aliasErr := queries.GetAllUserAliases(ctx)

	metadataMu.Lock()
	defer metadataMu.Unlock()

	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, fromDBUser(row))
		userCache[row.Email.String] = &users[len(users)-1]
	}

	if aliasErr == nil {
		mapAliasesToUsers(users, aliasRows)
	}

	allUsersCache = users
	allUsersCachedAt = time.Now()
	return users, nil
}

// InvalidateAllUsersCache drops the memoized user list so the next GetAllUsers
// hits the DB. Call from any user/alias write path.
func InvalidateAllUsersCache() {
	metadataMu.Lock()
	allUsersCache = nil
	allUsersCachedAt = time.Time{}
	metadataMu.Unlock()
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
	allUsersCache = nil
	allUsersCachedAt = time.Time{}
	metadataMu.Unlock()

	if name != "" {
		_ = AddUserAlias(ctx, u.ID, name)
	}

	return &u, nil
}

// UpdateUserWAJID updates the WhatsApp JID (identifier) associated with the user.
func UpdateUserWAJID(ctx context.Context, email, wajid string) error {
	if err := db.New(GetDB()).UpdateUserDetails(ctx, db.UpdateUserDetailsParams{
		Email: nullString(email),
		WaJid: sql.NullString{String: wajid, Valid: wajid != ""},
	}); err != nil {
		return err
	}
	InvalidateAllUsersCache()
	return nil
}

// UpdateUserSlackID updates the Slack ID associated with the user.
func UpdateUserSlackID(ctx context.Context, email, slackID string) error {
	if err := db.New(GetDB()).UpdateUserDetails(ctx, db.UpdateUserDetailsParams{
		Email:   nullString(email),
		SlackID: sql.NullString{String: slackID, Valid: slackID != ""},
	}); err != nil {
		return err
	}
	InvalidateAllUsersCache()
	return nil
}

// UpdateUserTgID updates the Telegram user ID associated with the user.
func UpdateUserTgID(ctx context.Context, email, tgUserID string) error {
	if err := db.New(GetDB()).UpdateUserDetails(ctx, db.UpdateUserDetailsParams{
		Email:    nullString(email),
		TgUserID: sql.NullString{String: tgUserID, Valid: tgUserID != ""},
	}); err != nil {
		return err
	}
	InvalidateAllUsersCache()
	return nil
}

func GetUserAliasesByEmail(ctx context.Context, email string) ([]string, error) {
	metadataMu.RLock()
	if u, ok := userCache[email]; ok && len(u.Aliases) > 0 {
		metadataMu.RUnlock()
		return u.Aliases, nil
	}
	metadataMu.RUnlock()

	aliases, err := db.New(GetDB()).GetUserAliasesByEmail(ctx, nullString(email))
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

func fromDBUser(row db.User) User {
	return User{
		ID:        UserID(row.ID),
		Email:     row.Email.String,
		Name:      row.Name.String,
		SlackID:   row.SlackID.String,
		WAJID:     row.WaJid.String,
		TgUserID:  row.TgUserID.String,
		Picture:   row.Picture.String,
		IsAdmin:   row.IsAdmin != 0,
		CreatedAt: row.CreatedAt.Time,
	}
}
