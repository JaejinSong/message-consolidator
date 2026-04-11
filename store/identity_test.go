package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestIdentityResolution(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := testutil.RandomEmail("ident")

	// 1. Add unique contacts
	aliasA := testutil.RandomID("alice")
	aliasB := testutil.RandomID("bob")
	aliasC := testutil.RandomID("charlie")

	id1, err1 := AddContact(ctx, tenant, testutil.RandomEmail("user_a"), "User A", aliasA, "manual")
	id2, err2 := AddContact(ctx, tenant, testutil.RandomEmail("user_b"), "User B", aliasB, "manual")
	id3, err3 := AddContact(ctx, tenant, testutil.RandomEmail("user_c"), "User C", aliasC, "manual")

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Failed to add contacts: %v, %v, %v", err1, err2, err3)
	}

	t.Logf("IDs: A=%d, B=%d, C=%d", id1, id2, id3)

	if id1 == 0 || id2 == 0 || id3 == 0 {
		t.Fatalf("Added contacts returned 0 IDs: %d, %d, %d", id1, id2, id3)
	}

	// 2. Link User A and User B
	err = LinkContact(ctx, tenant, int64(id1), int64(id2))
	if err != nil {
		t.Fatalf("Failed to link contacts: %v", err)
	}

	// 3. Resolve by alias (transitive)
	// A=B, so resolve aliasB should return ID of A (if A is master) or B (if B is master)
	// In our implementation, LinkContact (masterID, targetID) merges target into master.
	// So id2 is merged into id1. Resolve aliasB should return id1.
	res, _ := ResolveAlias(ctx, "name", aliasB)
	if res != id1 {
		t.Errorf("Expected resolved ID %d for '%s', got %d", id1, aliasB, res)
	}

	// 4. Test Transitivity: Link User B and User C
	// A=B, B=C => A=C
	_ = LinkContact(ctx, tenant, int64(id2), int64(id3))
	res2, _ := ResolveAlias(ctx, "name", aliasC)
	if res2 != id1 {
		t.Errorf("Expected resolved ID %d for '%s' (transitive), got %d", id1, aliasC, res2)
	}
}
