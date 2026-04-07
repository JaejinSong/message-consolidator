package store

import (
	"context"
	"message-consolidator/config"
	"testing"

	_ "modernc.org/sqlite"
)

func TestIdentityResolution(t *testing.T) {
	ctx := context.Background()
	
	// Setup: Reset DSU and Mock DB
	GlobalContactDSU = NewContactDSU()
	
	// Mock config for in-memory SQLite (libsql driver supports file::memory:?cache=shared)
	cfg := &config.Config{
		TursoURL: "file::memory:?cache=shared",
	}
	if err := InitDB(cfg); err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}
	InitContactsTable()

	// 1. Add unique contacts
	id1, err1 := AddContact(ctx, "test@example.com", "user_a", "User A", "alice,b", "manual")
	id2, err2 := AddContact(ctx, "test@example.com", "user_b", "User B", "bob", "manual")
	id3, err3 := AddContact(ctx, "test@example.com", "user_c", "User C", "charlie", "manual")

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Failed to add contacts: %v, %v, %v", err1, err2, err3)
	}

	t.Logf("IDs: A=%d, B=%d, C=%d", id1, id2, id3)

	if id1 == 0 || id2 == 0 || id3 == 0 {
		t.Fatalf("Added contacts returned 0 IDs: %d, %d, %d", id1, id2, id3)
	}

	// 2. Link User A and User B
	err := LinkContact(ctx, "test@example.com", int64(id1), int64(id2))
	if err != nil {
		t.Fatalf("Failed to link contacts: %v", err)
	}

	// 3. Resolve by alias (transitive)
	// A=B, so resolve "bob" should return ID of A (if A is master) or B (if B is master)
	// In our implementation, LinkContact (masterID, targetID) merges target into master.
	// So id2 is merged into id1. Resolve "bob" should return id1.
	res, _ := ResolveAlias(ctx, "name", "bob")
	if res != id1 {
		t.Errorf("Expected resolved ID %d for 'bob', got %d", id1, res)
	}

	// 4. Test Transitivity: Link User B and User C
	// A=B, B=C => A=C
	_ = LinkContact(ctx, "test@example.com", int64(id2), int64(id3))
	res2, _ := ResolveAlias(ctx, "name", "charlie")
	if res2 != id1 {
		t.Errorf("Expected resolved ID %d for 'charlie' (transitive), got %d", id1, res2)
	}
}
