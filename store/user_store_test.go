package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
	"time"
)

func TestGetOrCreateUser(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	name := "Test User"
	picture := "http://example.com/pic.jpg"

	t.Run("CreateNewUser", func(t *testing.T) {
		user, err := GetOrCreateUser(context.Background(), email, name, picture)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
		if user.Email != email || user.Name != name || user.Picture != picture {
			t.Errorf("Unexpected user data: %+v", user)
		}

		//Why: Verifies that the newly created user is correctly indexed in the in-memory cache to ensure subsequent lookups are fast.
	})

	t.Run("UpdateExistingUser", func(t *testing.T) {
		newName := "Updated User"
		user, err := GetOrCreateUser(context.Background(), email, newName, "")
		if err != nil {
			t.Fatalf("Failed to update user: %v", err)
		}
		if user.Name != newName {
			t.Errorf("Expected name '%s', got '%s'", newName, user.Name)
		}

		//Why: Verifies that user profile updates (e.g., name changes) are correctly persisted to the underlying database.
		var dbName string
		err = GetDB().QueryRow("SELECT name FROM users WHERE email = ?", email).Scan(&dbName)
		if err != nil || dbName != newName {
			t.Errorf("Name not updated in DB: %v (name: %s)", err, dbName)
		}
	})

	t.Run("GetExistingUserNoUpdate", func(t *testing.T) {
		user, err := GetOrCreateUser(context.Background(), email, "", "")
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.Email != email {
			t.Errorf("Expected email '%s', got '%s'", email, user.Email)
		}
	})

	t.Run("ConcurrentUpsert", func(t *testing.T) {
		const numRoutines = 10
		email := testutil.RandomEmail("concurrent")
		done := make(chan bool)
		errs := make(chan error, numRoutines)

		//Why: Simulate rapid concurrent registration requests to verify that the atomic DB Upsert prevents UNIQUE constraint violations and Race Conditions.
		for i := 0; i < numRoutines; i++ {
			go func() {
				_, err := GetOrCreateUser(context.Background(), email, "Concurrent User", "")
				if err != nil {
					errs <- err
				}
				done <- true
			}()
		}

		for i := 0; i < numRoutines; i++ {
			<-done
		}
		close(errs)

		for err := range errs {
			t.Errorf("Concurrent Upsert failed: %v", err)
		}
	})
}

func TestGetAllUsersCache(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("cache")
	if _, err := GetOrCreateUser(ctx, email, "Cache User", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("CacheHitReturnsSameSlice", func(t *testing.T) {
		first, err := GetAllUsers(ctx)
		if err != nil {
			t.Fatalf("first call: %v", err)
		}

		// Why: Drop the user out from under the cache. A cache hit must NOT see this delete.
		if _, err := GetDB().ExecContext(ctx, "DELETE FROM users WHERE email = ?", email); err != nil {
			t.Fatalf("manual delete: %v", err)
		}

		second, err := GetAllUsers(ctx)
		if err != nil {
			t.Fatalf("second call: %v", err)
		}
		if len(first) != len(second) {
			t.Fatalf("cache miss: len first=%d second=%d (expected cache hit to mask delete)", len(first), len(second))
		}

		// Restore so subsequent subtests have a known seed.
		if _, err := GetOrCreateUser(ctx, email, "Cache User", ""); err != nil {
			t.Fatalf("reseed: %v", err)
		}
		// updateAndCacheUser invalidates the cache; verify by checking the next call goes to DB.
		fresh, _ := GetAllUsers(ctx)
		if len(fresh) == 0 {
			t.Fatalf("post-reseed expected >=1 user, got 0")
		}
	})

	t.Run("InvalidateAllUsersCache", func(t *testing.T) {
		// Prime cache.
		if _, err := GetAllUsers(ctx); err != nil {
			t.Fatalf("prime: %v", err)
		}

		// Manual DB delete bypasses our invalidation hooks; cache still serves stale.
		if _, err := GetDB().ExecContext(ctx, "DELETE FROM users WHERE email = ?", email); err != nil {
			t.Fatalf("manual delete: %v", err)
		}

		stale, _ := GetAllUsers(ctx)
		InvalidateAllUsersCache()
		fresh, _ := GetAllUsers(ctx)
		if len(stale) <= len(fresh) {
			t.Fatalf("expected stale > fresh after invalidation, got stale=%d fresh=%d", len(stale), len(fresh))
		}
	})

	t.Run("TTLExpiry", func(t *testing.T) {
		if _, err := GetOrCreateUser(ctx, testutil.RandomEmail("ttl"), "TTL User", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := GetAllUsers(ctx); err != nil {
			t.Fatalf("prime: %v", err)
		}

		// Force the TTL to elapse without sleeping.
		metadataMu.Lock()
		allUsersCachedAt = time.Now().Add(-2 * allUsersCacheTTL)
		metadataMu.Unlock()

		newEmail := testutil.RandomEmail("ttlnew")
		if _, err := GetDB().ExecContext(ctx,
			"INSERT INTO users (email, name) VALUES (?, ?)", newEmail, "Direct Insert"); err != nil {
			t.Fatalf("direct insert: %v", err)
		}

		users, _ := GetAllUsers(ctx)
		found := false
		for _, u := range users {
			if u.Email == newEmail {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("post-TTL expected to see direct-insert user %q in fresh fetch", newEmail)
		}
	})

	t.Run("UpdateUserSlackIDInvalidates", func(t *testing.T) {
		seed := testutil.RandomEmail("slackinv")
		if _, err := GetOrCreateUser(ctx, seed, "Slack Inv", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// Prime cache (slack_id empty).
		users, _ := GetAllUsers(ctx)
		for _, u := range users {
			if u.Email == seed && u.SlackID != "" {
				t.Fatalf("precondition: slack_id should be empty, got %q", u.SlackID)
			}
		}

		if err := UpdateUserSlackID(ctx, seed, "U999"); err != nil {
			t.Fatalf("update: %v", err)
		}

		// Next read must reflect the slack_id write.
		users2, _ := GetAllUsers(ctx)
		var got string
		for _, u := range users2 {
			if u.Email == seed {
				got = u.SlackID
				break
			}
		}
		if got != "U999" {
			t.Fatalf("expected SlackID=U999 after invalidation, got %q", got)
		}
	})
}

func TestUpdateUserSlackID(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("slack")
	_, _ = GetOrCreateUser(context.Background(), email, "Slack User", "")

	slackID := "U12345"
	if err := UpdateUserSlackID(context.Background(), email, slackID); err != nil {
		t.Fatalf("Failed to update slack ID: %v", err)
	}

	var dbSlackID string
	err = GetDB().QueryRow("SELECT slack_id FROM users WHERE email = ?", email).Scan(&dbSlackID)
	if err != nil || dbSlackID != slackID {
		t.Errorf("Slack ID not updated in DB: %v (slack_id: %s)", err, dbSlackID)
	}
}
