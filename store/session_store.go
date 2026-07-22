package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"message-consolidator/db"
)

// ErrSessionNotFound is returned when a session token is missing or expired.
var ErrSessionNotFound = errors.New("session not found or expired")

// CreateSession persists an opaque server-side session token bound to email.
func CreateSession(ctx context.Context, token, email string, expiresAt time.Time) error {
	return db.New(GetDB()).CreateSession(ctx, db.CreateSessionParams{
		Token:     token,
		Email:     email,
		ExpiresAt: expiresAt,
	})
}

// GetSessionEmail resolves a session token to its email. Returns ErrSessionNotFound
// when the token is unknown or already expired (GetSession filters expired rows in SQL).
func GetSessionEmail(ctx context.Context, token string) (string, error) {
	row, err := db.New(GetDB()).GetSession(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrSessionNotFound
		}
		return "", err
	}
	return row.Email, nil
}

// DeleteSession removes a single session, e.g. on logout.
func DeleteSession(ctx context.Context, token string) error {
	return db.New(GetDB()).DeleteSession(ctx, token)
}

// DeleteExpiredSessions purges expired rows to keep the table bounded.
func DeleteExpiredSessions(ctx context.Context) error {
	return db.New(GetDB()).DeleteExpiredSessions(ctx)
}
