package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestCreateAndCheckGrant(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	grantor, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantor"), "Grantor User", "")
	if err != nil {
		t.Fatalf("Failed to create grantor: %v", err)
	}
	grantee, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantee"), "Grantee User", "")
	if err != nil {
		t.Fatalf("Failed to create grantee: %v", err)
	}

	t.Run("BeforeGrant_NotGranted", func(t *testing.T) {
		granted, err := IsGrantedToView(ctx, grantee.ID, grantor.ID)
		if err != nil {
			t.Fatalf("IsGrantedToView before grant: %v", err)
		}
		if granted {
			t.Errorf("expected false before grant, got true")
		}
	})

	t.Run("AfterCreateGrant_IsGranted", func(t *testing.T) {
		if err := CreateGrant(ctx, grantor.ID, grantee.ID); err != nil {
			t.Fatalf("CreateGrant: %v", err)
		}
		granted, err := IsGrantedToView(ctx, grantee.ID, grantor.ID)
		if err != nil {
			t.Fatalf("IsGrantedToView after grant: %v", err)
		}
		if !granted {
			t.Errorf("expected true after grant, got false")
		}
	})

	t.Run("IdempotentCreateGrant_NoError", func(t *testing.T) {
		if err := CreateGrant(ctx, grantor.ID, grantee.ID); err != nil {
			t.Fatalf("CreateGrant idempotent call: %v", err)
		}
		granted, err := IsGrantedToView(ctx, grantee.ID, grantor.ID)
		if err != nil {
			t.Fatalf("IsGrantedToView after second CreateGrant: %v", err)
		}
		if !granted {
			t.Errorf("expected true after idempotent CreateGrant, got false")
		}
	})
}

func TestRevokeGrant(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	grantor, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantor"), "Grantor User", "")
	if err != nil {
		t.Fatalf("Failed to create grantor: %v", err)
	}
	grantee, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantee"), "Grantee User", "")
	if err != nil {
		t.Fatalf("Failed to create grantee: %v", err)
	}

	if err := CreateGrant(ctx, grantor.ID, grantee.ID); err != nil {
		t.Fatalf("CreateGrant setup: %v", err)
	}

	t.Run("AfterRevoke_NotGranted", func(t *testing.T) {
		if err := RevokeGrant(ctx, grantor.ID, grantee.ID); err != nil {
			t.Fatalf("RevokeGrant: %v", err)
		}
		granted, err := IsGrantedToView(ctx, grantee.ID, grantor.ID)
		if err != nil {
			t.Fatalf("IsGrantedToView after revoke: %v", err)
		}
		if granted {
			t.Errorf("expected false after revoke, got true")
		}
	})

	t.Run("DoubleRevoke_NoError", func(t *testing.T) {
		if err := RevokeGrant(ctx, grantor.ID, grantee.ID); err != nil {
			t.Fatalf("RevokeGrant second call: %v", err)
		}
	})
}

func TestIsGrantedToView_Asymmetric(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	userA, err := GetOrCreateUser(ctx, testutil.RandomEmail("userA"), "User A", "")
	if err != nil {
		t.Fatalf("Failed to create userA: %v", err)
	}
	userB, err := GetOrCreateUser(ctx, testutil.RandomEmail("userB"), "User B", "")
	if err != nil {
		t.Fatalf("Failed to create userB: %v", err)
	}

	// A grants B view access to A's tasks.
	if err := CreateGrant(ctx, userA.ID, userB.ID); err != nil {
		t.Fatalf("CreateGrant A→B: %v", err)
	}

	t.Run("B_CanView_A", func(t *testing.T) {
		granted, err := IsGrantedToView(ctx, userB.ID, userA.ID)
		if err != nil {
			t.Fatalf("IsGrantedToView(B, A): %v", err)
		}
		if !granted {
			t.Errorf("expected B to be granted view of A's tasks, got false")
		}
	})

	t.Run("A_CannotView_B", func(t *testing.T) {
		granted, err := IsGrantedToView(ctx, userA.ID, userB.ID)
		if err != nil {
			t.Fatalf("IsGrantedToView(A, B): %v", err)
		}
		if granted {
			t.Errorf("expected A NOT to be granted view of B's tasks (one-way grant), got true")
		}
	})
}
