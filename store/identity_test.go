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
	emailA := testutil.RandomEmail("user_a")
	emailB := testutil.RandomEmail("user_b")
	emailC := testutil.RandomEmail("user_c")

	id1, err1 := AddContact(ctx, tenant, emailA, "User A", "", "manual")
	id2, err2 := AddContact(ctx, tenant, emailB, "User B", "", "manual")
	id3, err3 := AddContact(ctx, tenant, emailC, "User C", "", "manual")

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Failed to add contacts: %v, %v, %v", err1, err2, err3)
	}

	t.Logf("IDs: A=%d, B=%d, C=%d", id1, id2, id3)

	if id1 == 0 || id2 == 0 || id3 == 0 {
		t.Fatalf("Added contacts returned 0 IDs: %d, %d, %d", id1, id2, id3)
	}

	// 2. Link User B into User A (A is master)
	err = LinkContact(ctx, tenant, int64(id1), int64(id2))
	if err != nil {
		t.Fatalf("Failed to link contacts: %v", err)
	}

	// 3. Resolve emailB — DSU maps id2 → id1
	res, _ := ResolveAlias(ctx, ContactTypeEmail, emailB)
	if res != id1 {
		t.Errorf("Expected resolved ID %d for '%s', got %d", id1, emailB, res)
	}

	// 4. Transitivity: B=C, A=B=C → all resolve to id1
	_ = LinkContact(ctx, tenant, int64(id2), int64(id3))
	res2, _ := ResolveAlias(ctx, ContactTypeEmail, emailC)
	if res2 != id1 {
		t.Errorf("Expected resolved ID %d for '%s' (transitive), got %d", id1, emailC, res2)
	}
}
