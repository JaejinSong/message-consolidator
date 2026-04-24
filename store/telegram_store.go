package store

import (
	"context"
	"database/sql"
	"errors"
	"message-consolidator/db"
)

// GetTelegramSession returns the raw session bytes previously persisted by gotd.
// Returns (nil, nil) when no session exists so the caller can treat it as "no session yet".
func GetTelegramSession(ctx context.Context, email string) ([]byte, error) {
	data, err := db.New(GetDB()).GetTelegramSession(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// UpsertTelegramSession persists the session payload produced by gotd's SessionStorage.
func UpsertTelegramSession(ctx context.Context, email string, data []byte) error {
	return db.New(GetDB()).UpsertTelegramSession(ctx, db.UpsertTelegramSessionParams{
		Email:       email,
		SessionData: data,
	})
}

// DeleteTelegramSession removes the stored session — called after an explicit logout.
func DeleteTelegramSession(ctx context.Context, email string) error {
	return db.New(GetDB()).DeleteTelegramSession(ctx, email)
}
