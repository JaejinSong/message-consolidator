package store

import (
	"message-consolidator/internal/testutil"
	"strings"
	"testing"
)

func TestAutoUpsertContact(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := "tenant@whatap.io"

	email := "user1@whatap.io"
	
	// 1. Initial Insert
	err = AutoUpsertContact(tenant, email, "User One", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}

	
	name := NormalizeContactName(tenant, email)
	if name != "User One" {
		t.Errorf("Expected User One, got %s", name)
	}

	// 2. Defensive Update: Empty Name
	err = AutoUpsertContact(tenant, email, "", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(tenant, email)
	if name != "User One" {
		t.Errorf("Defensive update failed: Expected User One, got %s", name)
	}

	// 3. Defensive Update: Name = Email
	err = AutoUpsertContact(tenant, email, "user1@whatap.io", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(tenant, email)
	if name != "User One" {
		t.Errorf("Defensive update (email) failed: Expected User One, got %s", name)
	}

	// 4. Update with New Valid Name & Merge Alias
	err = AutoUpsertContact(tenant, email, "User 1", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(tenant, email)
	if name != "User 1" {
		t.Errorf("Update failed: Expected User 1, got %s", name)
	}

	// Check aliases merged
	metadataMu.RLock()
	var aliases string
	for _, m := range contactsCache[tenant] {
		if m.CanonicalID == email {
			aliases = m.Aliases
			break
		}
	}
	metadataMu.RUnlock()

	if !strings.Contains(aliases, "User One") || !strings.Contains(aliases, "User 1") {
		t.Errorf("Aliases merge failed: got %s", aliases)
	}
}
