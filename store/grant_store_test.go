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

func TestListGranteesOf(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()

	grantor, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantor"), "Grantor", "")
	if err != nil {
		t.Fatalf("Failed to create grantor: %v", err)
	}
	grantee1, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantee1"), "Grantee One", "")
	if err != nil {
		t.Fatalf("Failed to create grantee1: %v", err)
	}
	grantee2, err := GetOrCreateUser(ctx, testutil.RandomEmail("grantee2"), "Grantee Two", "")
	if err != nil {
		t.Fatalf("Failed to create grantee2: %v", err)
	}

	if err := CreateGrant(ctx, grantor.ID, grantee1.ID); err != nil {
		t.Fatalf("CreateGrant grantee1: %v", err)
	}
	if err := CreateGrant(ctx, grantor.ID, grantee2.ID); err != nil {
		t.Fatalf("CreateGrant grantee2: %v", err)
	}

	t.Run("TwoGrantees_ReturnsBoth", func(t *testing.T) {
		grantees, err := ListGranteesOf(ctx, grantor.ID)
		if err != nil {
			t.Fatalf("ListGranteesOf: %v", err)
		}
		if len(grantees) != 2 {
			t.Fatalf("expected 2 grantees, got %d", len(grantees))
		}
		emailSet := map[string]bool{
			grantee1.Email: true,
			grantee2.Email: true,
		}
		for _, u := range grantees {
			if !emailSet[u.Email] {
				t.Errorf("unexpected grantee email %q in result", u.Email)
			}
		}
	})

	t.Run("AfterRevokeOne_ReturnsOne", func(t *testing.T) {
		if err := RevokeGrant(ctx, grantor.ID, grantee1.ID); err != nil {
			t.Fatalf("RevokeGrant grantee1: %v", err)
		}
		grantees, err := ListGranteesOf(ctx, grantor.ID)
		if err != nil {
			t.Fatalf("ListGranteesOf after revoke: %v", err)
		}
		if len(grantees) != 1 {
			t.Fatalf("expected 1 grantee after revoke, got %d", len(grantees))
		}
		if grantees[0].Email != grantee2.Email {
			t.Errorf("expected remaining grantee email %q, got %q", grantee2.Email, grantees[0].Email)
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
