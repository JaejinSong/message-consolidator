package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestAutoUpsertContact(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := testutil.RandomEmail("contact-tenant")
	email := testutil.RandomEmail("contact-user")
	
	// 1. Initial Insert
	err = AutoUpsertContact(context.Background(), tenant, email, "User One", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}

	
	name := NormalizeContactName(tenant, email)
	if name != "User One" {
		t.Errorf("Expected User One, got %s", name)
	}

	// 2. Defensive Update: Empty Name
	err = AutoUpsertContact(context.Background(), tenant, email, "", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(tenant, email)
	if name != "User One" {
		t.Errorf("Defensive update failed: Expected User One, got %s", name)
	}

	// 3. Defensive Update: Name = Email
	err = AutoUpsertContact(context.Background(), tenant, email, "user1@whatap.io", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(tenant, email)
	if name != "User One" {
		t.Errorf("Defensive update (email) failed: Expected User One, got %s", name)
	}

	// 4. Update with New Valid Name & Merge Alias
	err = AutoUpsertContact(context.Background(), tenant, email, "User 1", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(tenant, email)
	if name != "User 1" {
		t.Errorf("Update failed: Expected User 1, got %s", name)
	}

	// Verify contact exists with the latest display_name
	var displayName string
	err = GetDB().QueryRow("SELECT display_name FROM contacts WHERE canonical_id = ?", email).Scan(&displayName)
	if err != nil {
		t.Fatalf("Failed to query contact: %v", err)
	}
	if displayName != "User 1" {
		t.Errorf("Expected display_name 'User 1', got '%s'", displayName)
	}
}
