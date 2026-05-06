package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"message-consolidator/db"
)

// CreateGrant records that grantorID allows granteeID to view their tasks.
func CreateGrant(ctx context.Context, grantorID, granteeID UserID) error {
	err := db.New(GetDB()).CreateGrant(ctx, db.CreateGrantParams{
		GrantorUserID: int64(grantorID),
		GranteeUserID: int64(granteeID),
	})
	if err != nil {
		return fmt.Errorf("create grant %d→%d: %w", grantorID, granteeID, err)
	}
	return nil
}

// RevokeGrant removes the view permission previously granted by grantorID to granteeID.
func RevokeGrant(ctx context.Context, grantorID, granteeID UserID) error {
	err := db.New(GetDB()).DeleteGrant(ctx, db.DeleteGrantParams{
		GrantorUserID: int64(grantorID),
		GranteeUserID: int64(granteeID),
	})
	if err != nil {
		return fmt.Errorf("revoke grant %d→%d: %w", grantorID, granteeID, err)
	}
	return nil
}

// IsGrantedToView reports whether grantorID has allowed granteeID to view their tasks.
// Returns false, nil when no grant row exists.
func IsGrantedToView(ctx context.Context, granteeID, grantorID UserID) (bool, error) {
	_, err := db.New(GetDB()).GetGrant(ctx, db.GetGrantParams{
		GrantorUserID: int64(grantorID),
		GranteeUserID: int64(granteeID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check grant %d→%d: %w", grantorID, granteeID, err)
	}
	return true, nil
}

// ListGranteesOf returns all users that grantorID has granted view access to.
func ListGranteesOf(ctx context.Context, grantorID UserID) ([]User, error) {
	rows, err := db.New(GetDB()).ListGranteesOf(ctx, int64(grantorID))
	if err != nil {
		return nil, fmt.Errorf("list grantees of %d: %w", grantorID, err)
	}
	users := make([]User, 0, len(rows))
	for _, row := range rows {
		users = append(users, fromDBUser(row))
	}
	return users, nil
}
