package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

// TestGetContactsByIdentifiers_MasterFollowing verifies that GetContactsByIdentifiers
// correctly follows master_contact_id links and preserves DisplayName when the master has none.
func TestGetContactsByIdentifiers_MasterFollowing(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("tenant")

	masterID, err := AddContact(ctx, tenant, "master@example.com", "Master Name", "", "test")
	if err != nil {
		t.Fatalf("add master: %v", err)
	}
	slaveID, err := AddContact(ctx, tenant, "slave@example.com", "Slave Name", "", "test")
	if err != nil {
		t.Fatalf("add slave: %v", err)
	}
	if err := LinkContact(ctx, tenant, masterID, slaveID); err != nil {
		t.Fatalf("link: %v", err)
	}

	t.Run("resolves slave identifier to master CanonicalID", func(t *testing.T) {
		res, _, err := GetContactsByIdentifiers(ctx, tenant, []string{"slave@example.com"})
		if err != nil {
			t.Fatalf("GetContactsByIdentifiers: %v", err)
		}
		c, ok := res["slave@example.com"]
		if !ok || c == nil {
			t.Fatal("expected slave identifier to resolve")
		}
		if c.CanonicalID != "master@example.com" {
			t.Errorf("expected CanonicalID=master@example.com, got %s", c.CanonicalID)
		}
	})

	t.Run("uses master DisplayName when non-empty", func(t *testing.T) {
		res, _, _ := GetContactsByIdentifiers(ctx, tenant, []string{"slave@example.com"})
		c := res["slave@example.com"]
		if c == nil {
			t.Fatal("not resolved")
		}
		if c.DisplayName != "Master Name" {
			t.Errorf("expected DisplayName=Master Name, got %s", c.DisplayName)
		}
	})

	t.Run("preserves slave DisplayName when master DisplayName is empty", func(t *testing.T) {
		// Create a master with no DisplayName, link a slave that has one.
		emptyMasterID, _ := AddContact(ctx, tenant, "empty-master@example.com", "", "", "test")
		namedSlaveID, _ := AddContact(ctx, tenant, "named-slave@example.com", "Named Slave", "", "test")
		if err := LinkContact(ctx, tenant, emptyMasterID, namedSlaveID); err != nil {
			t.Fatalf("link empty master: %v", err)
		}

		res, _, _ := GetContactsByIdentifiers(ctx, tenant, []string{"named-slave@example.com"})
		c := res["named-slave@example.com"]
		if c == nil {
			t.Fatal("not resolved")
		}
		if c.DisplayName != "Named Slave" {
			t.Errorf("expected slave DisplayName=Named Slave to be preserved, got %q", c.DisplayName)
		}
		if c.CanonicalID != "empty-master@example.com" {
			t.Errorf("expected CanonicalID=empty-master@example.com, got %s", c.CanonicalID)
		}
	})

	t.Run("two slaves with same master unify to same node", func(t *testing.T) {
		slave2ID, _ := AddContact(ctx, tenant, "slave2@example.com", "Slave 2", "", "test")
		if err := LinkContact(ctx, tenant, masterID, slave2ID); err != nil {
			t.Fatalf("link slave2: %v", err)
		}

		res, _, _ := GetContactsByIdentifiers(ctx, tenant, []string{"slave@example.com", "slave2@example.com"})
		c1 := res["slave@example.com"]
		c2 := res["slave2@example.com"]
		if c1 == nil || c2 == nil {
			t.Fatal("one of the slaves not resolved")
		}
		if c1.CanonicalID != c2.CanonicalID {
			t.Errorf("expected same CanonicalID for siblings, got %s vs %s", c1.CanonicalID, c2.CanonicalID)
		}
	})
}

func TestLinkContact_SelfLink(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("tenant")
	id, _ := AddContact(ctx, tenant, "a@example.com", "A", "", "test")
	if err := LinkContact(ctx, tenant, id, id); err == nil {
		t.Error("expected error when linking contact to itself")
	}
}

func TestLinkContact_CircularReference(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("tenant")
	masterID, _ := AddContact(ctx, tenant, "master@example.com", "Master", "", "test")
	slaveID, _ := AddContact(ctx, tenant, "slave@example.com", "Slave", "", "test")
	if err := LinkContact(ctx, tenant, masterID, slaveID); err != nil {
		t.Fatalf("initial link: %v", err)
	}
	// Attempt to link master → slave's master (which is master itself) should fail.
	if err := LinkContact(ctx, tenant, slaveID, masterID); err == nil {
		t.Error("expected circular reference error")
	}
}

func TestBuildAliasQuery(t *testing.T) {
	cases := []struct {
		idType  string
		wantArgs int
	}{
		{ContactTypeWhatsApp, 2},
		{ContactTypeEmail, 1},
		{"other", 2},
	}
	for _, tc := range cases {
		q, args := buildAliasQuery(tc.idType, "test")
		if q == "" {
			t.Errorf("idType=%s: empty query", tc.idType)
		}
		if len(args) != tc.wantArgs {
			t.Errorf("idType=%s: want %d args, got %d", tc.idType, tc.wantArgs, len(args))
		}
	}
}

func TestDeduplicateByDSU(t *testing.T) {
	// With a fresh DSU, each ID maps to itself, so duplicates should collapse.
	ids := []int64{1, 2, 1, 3, 2}
	result := deduplicateByDSU(ids)
	seen := make(map[int64]bool)
	for _, id := range result {
		if seen[id] {
			t.Errorf("duplicate id %d in result", id)
		}
		seen[id] = true
	}
}

func TestSaveWhatsAppContact_SkipsEmptyOrSameAsNumber(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("tenant")
	if err := SaveWhatsAppContact(ctx, tenant, "", "Name"); err != nil {
		t.Errorf("empty number should return nil, got %v", err)
	}
	if err := SaveWhatsAppContact(ctx, tenant, "123", "123"); err != nil {
		t.Errorf("name==number should return nil, got %v", err)
	}
}

func TestAutoUpsertContact(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := t.Context()
	tenant := testutil.RandomEmail("contact-tenant")
	email := testutil.RandomEmail("contact-user")
	
	// 1. Initial Insert
	err = AutoUpsertContact(context.Background(), tenant, email, "User One", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}

	
	name := NormalizeContactName(ctx, tenant, email)
	if name != "User One" {
		t.Errorf("Expected User One, got %s", name)
	}

	// 2. Defensive Update: Empty Name
	err = AutoUpsertContact(context.Background(), tenant, email, "", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(ctx, tenant, email)
	if name != "User One" {
		t.Errorf("Defensive update failed: Expected User One, got %s", name)
	}

	// 3. Defensive Update: Name = Email
	err = AutoUpsertContact(context.Background(), tenant, email, "user1@whatap.io", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(ctx, tenant, email)
	if name != "User One" {
		t.Errorf("Defensive update (email) failed: Expected User One, got %s", name)
	}

	// 4. Update with New Valid Name & Merge Alias
	err = AutoUpsertContact(context.Background(), tenant, email, "User 1", "test")
	if err != nil {
		t.Fatalf("Failed to upsert: %v", err)
	}
	name = NormalizeContactName(ctx, tenant, email)
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
