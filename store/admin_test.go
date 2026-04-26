package store

import (
	"context"
	"errors"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestIsSuperAdmin(t *testing.T) {
	if !IsSuperAdmin("jjsong@whatap.io") {
		t.Errorf("expected hardcoded super admin to match")
	}
	if !IsSuperAdmin("JJSONG@whatap.io") {
		t.Errorf("super admin match should be case-insensitive")
	}
	if IsSuperAdmin("other@whatap.io") {
		t.Errorf("non-super email should not match")
	}
	if IsSuperAdmin("") {
		t.Errorf("empty input should not match super admin")
	}
}

func TestSetUserAdminAndIsAdmin(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "deleg@example.com"
	if _, err := GetOrCreateUser(ctx, email, "Delegate", ""); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if IsAdmin(ctx, email) {
		t.Fatalf("fresh user should not be admin")
	}
	if err := SetUserAdmin(ctx, email, true); err != nil {
		t.Fatalf("grant admin: %v", err)
	}
	if !IsAdmin(ctx, email) {
		t.Fatalf("expected user to be admin after grant")
	}
	if err := SetUserAdmin(ctx, email, false); err != nil {
		t.Fatalf("revoke admin: %v", err)
	}
	if IsAdmin(ctx, email) {
		t.Fatalf("expected user to lose admin after revoke")
	}
}

func TestSetUserAdminRefusesSuper(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	if err := SetUserAdmin(context.Background(), SuperAdminEmail, false); !errors.Is(err, ErrSuperAdminImmutable) {
		t.Fatalf("expected ErrSuperAdminImmutable, got %v", err)
	}
}

func TestSettingsCacheRoundtrip(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	InvalidateSettingsCache()

	if v := GetSetting(ctx, "LOG_LEVEL", "INFO"); v != "INFO" {
		t.Fatalf("expected fallback INFO, got %q", v)
	}

	if err := UpsertSetting(ctx, "LOG_LEVEL", "DEBUG", "tester@example.com"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if v := GetSetting(ctx, "LOG_LEVEL", "INFO"); v != "DEBUG" {
		t.Fatalf("expected stored DEBUG, got %q", v)
	}

	if err := DeleteSetting(ctx, "LOG_LEVEL"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if v := GetSetting(ctx, "LOG_LEVEL", "INFO"); v != "INFO" {
		t.Fatalf("expected fallback after delete, got %q", v)
	}
}
