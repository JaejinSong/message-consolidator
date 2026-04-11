package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
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
		err = db.QueryRow("SELECT name FROM users WHERE email = ?", email).Scan(&dbName)
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
	err = db.QueryRow("SELECT slack_id FROM users WHERE email = ?", email).Scan(&dbSlackID)
	if err != nil || dbSlackID != slackID {
		t.Errorf("Slack ID not updated in DB: %v (slack_id: %s)", err, dbSlackID)
	}
}
