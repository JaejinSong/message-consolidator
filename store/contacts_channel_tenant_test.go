package store

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
)

// TestGetNameByWhatsAppNumber_TenantIsolation is the regression for a cross-tenant
// contact leak. Why: buildAliasQuery resolves canonical_id/secondary_ids with no
// tenant_email predicate, and getNameByExternalID used to return the resolved row's
// DisplayName without re-checking ownership -- so a number labelled only by tenant B
// surfaced B's private contact name inside tenant A's messages and extracted tasks.
// Measured 2026-09-10: 626 of 627 production identifiers were single-tenant, i.e. the
// asymmetric case that leaks (a number known to both tenants resolves ambiguously and
// already failed safe).
func TestGetNameByWhatsAppNumber_TenantIsolation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenantA := testutil.RandomEmail("tenant-a")
	tenantB := testutil.RandomEmail("tenant-b")
	const number = "6281234567890"

	// Only tenant B labels this number.
	if _, err := AddContact(ctx, tenantB, number, "Jane Doe, CFO of Acme", "", "test"); err != nil {
		t.Fatalf("add tenant B contact: %v", err)
	}

	if got := GetNameByWhatsAppNumber(ctx, tenantA, number); got != "" {
		t.Errorf("tenant A resolved %q; want empty -- that is tenant B's contact name", got)
	}
	if got := GetNameByWhatsAppNumber(ctx, tenantB, number); got != "Jane Doe, CFO of Acme" {
		t.Errorf("tenant B resolved %q; want its own contact name", got)
	}
}

// TestGetNameByTelegramID_TenantIsolation covers the same leak on the Telegram path,
// which funnels through the same getNameByExternalID helper.
func TestGetNameByTelegramID_TenantIsolation(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenantA := testutil.RandomEmail("tenant-a")
	tenantB := testutil.RandomEmail("tenant-b")
	const userID = "778899001"

	if _, err := AddContact(ctx, tenantB, userID, "Internal Roadmap Owner", "", "test"); err != nil {
		t.Fatalf("add tenant B contact: %v", err)
	}

	if got := GetNameByTelegramID(ctx, tenantA, userID); got != "" {
		t.Errorf("tenant A resolved %q; want empty", got)
	}
	if got := GetNameByTelegramID(ctx, tenantB, userID); got != "Internal Roadmap Owner" {
		t.Errorf("tenant B resolved %q; want its own contact name", got)
	}
}
