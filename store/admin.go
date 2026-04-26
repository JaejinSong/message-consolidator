package store

import (
	"context"
	"database/sql"
	"errors"
	"message-consolidator/db"
	"strings"
)

// ErrSuperAdminImmutable is returned when a caller attempts to revoke or alter the super admin role.
var ErrSuperAdminImmutable = errors.New("super admin role cannot be modified")

// SuperAdminEmail is the hardcoded super-administrator. Cannot be revoked through the UI.
// Why: defined in the store package because both auth and migrations need it; auth imports store
// (not the reverse), so any auth-side constant would create an import cycle.
const SuperAdminEmail = "jjsong@whatap.io"

// IsSuperAdmin reports whether the given email is the hardcoded super admin (case-insensitive).
func IsSuperAdmin(email string) bool {
	return strings.EqualFold(strings.TrimSpace(email), SuperAdminEmail)
}

// IsAdmin reports whether the given email is an administrator (super admin OR users.is_admin=1).
// Returns false on lookup error or empty input.
func IsAdmin(ctx context.Context, email string) bool {
	if email == "" {
		return false
	}
	if IsSuperAdmin(email) {
		return true
	}
	conn := GetDB()
	if conn == nil {
		return false
	}
	row, err := db.New(conn).GetUserByEmail(ctx, sql.NullString{String: email, Valid: true})
	if err != nil {
		return false
	}
	return row.IsAdmin != 0
}

// SetUserAdmin grants or revokes admin privileges for a non-super-admin user.
// Returns an error when the target is the super admin (immutable) or when the user does not exist.
func SetUserAdmin(ctx context.Context, email string, grant bool) error {
	if IsSuperAdmin(email) {
		return ErrSuperAdminImmutable
	}
	conn := GetDB()
	flag := int64(0)
	if grant {
		flag = 1
	}
	if err := db.New(conn).SetUserAdmin(ctx, db.SetUserAdminParams{
		Email:   sql.NullString{String: email, Valid: true},
		IsAdmin: flag,
	}); err != nil {
		return err
	}
	InvalidateAllUsersCache()
	return nil
}

// ListAdmins returns all administrators (super admin always first, followed by delegated admins).
// The super admin entry is synthesized when no DB row exists yet (e.g., before first login).
func ListAdmins(ctx context.Context) ([]User, error) {
	conn := GetDB()
	rows, err := db.New(conn).ListAdminUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(rows)+1)
	superSeen := false
	for _, r := range rows {
		u := fromDBUser(r)
		if IsSuperAdmin(u.Email) {
			superSeen = true
			out = append([]User{u}, out...)
			continue
		}
		out = append(out, u)
	}
	if !superSeen {
		out = append([]User{{Email: SuperAdminEmail, IsAdmin: true}}, out...)
	}
	return out, nil
}
