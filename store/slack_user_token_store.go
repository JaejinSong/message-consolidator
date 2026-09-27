package store

import (
	"context"
	"database/sql"
	"errors"

	"message-consolidator/db"
)

// SlackUserToken is a decrypted per-user Slack OAuth (xoxp) grant, cached in memory
// keyed by user email alongside the Gmail token cache.
type SlackUserToken struct {
	Token       string
	SlackUserID string
	Scopes      string
}

// SaveSlackUserToken persists an encrypted per-user Slack token and caches the
// plaintext, mirroring SaveGmailToken.
func SaveSlackUserToken(ctx context.Context, email string, t SlackUserToken) error {
	metadataMu.Lock()
	slackUserTokenCache[email] = t
	metadataMu.Unlock()

	conn := GetDB()
	queries := db.New(conn)
	return queries.UpsertSlackUserToken(ctx, db.UpsertSlackUserTokenParams{
		UserEmail:   email,
		TokenEnc:    encryptString(t.Token),
		SlackUserID: t.SlackUserID,
		Scopes:      t.Scopes,
	})
}

// GetSlackUserToken returns the per-user Slack token, cache-first. It returns
// (zero, false, nil) when no token is stored for email, rather than an error.
func GetSlackUserToken(ctx context.Context, email string) (SlackUserToken, bool, error) {
	metadataMu.RLock()
	t, ok := slackUserTokenCache[email]
	metadataMu.RUnlock()
	if ok {
		return t, true, nil
	}

	conn := GetDB()
	queries := db.New(conn)
	row, err := queries.GetSlackUserToken(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return SlackUserToken{}, false, nil
	}
	if err != nil {
		return SlackUserToken{}, false, err
	}

	t = SlackUserToken{
		Token:       decryptString(row.TokenEnc),
		SlackUserID: row.SlackUserID,
		Scopes:      row.Scopes,
	}

	metadataMu.Lock()
	slackUserTokenCache[email] = t
	metadataMu.Unlock()

	return t, true, nil
}

// DeleteSlackUserToken removes the per-user Slack token from cache and DB.
func DeleteSlackUserToken(ctx context.Context, email string) error {
	metadataMu.Lock()
	delete(slackUserTokenCache, email)
	metadataMu.Unlock()

	conn := GetDB()
	queries := db.New(conn)
	return queries.DeleteSlackUserToken(ctx, email)
}

// HasSlackUserToken reports cache-only presence, matching HasGmailToken's semantics.
func HasSlackUserToken(email string) bool {
	metadataMu.RLock()
	_, ok := slackUserTokenCache[email]
	metadataMu.RUnlock()
	return ok
}
