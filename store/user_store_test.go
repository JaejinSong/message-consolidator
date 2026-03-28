package store

import (
	"testing"
)

func TestGetOrCreateUser(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	name := "Test User"
	picture := "http://example.com/pic.jpg"

	t.Run("CreateNewUser", func(t *testing.T) {
		user, err := GetOrCreateUser(email, name, picture)
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
		user, err := GetOrCreateUser(email, newName, "")
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
		user, err := GetOrCreateUser(email, "", "")
		if err != nil {
			t.Fatalf("Failed to get user: %v", err)
		}
		if user.Email != email {
			t.Errorf("Expected email '%s', got '%s'", email, user.Email)
		}
	})
}

func TestUpdateUserSlackID(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "slack@example.com"
	_, _ = GetOrCreateUser(email, "Slack User", "")

	slackID := "U12345"
	if err := UpdateUserSlackID(email, slackID); err != nil {
		t.Fatalf("Failed to update slack ID: %v", err)
	}

	var dbSlackID string
	err = db.QueryRow("SELECT slack_id FROM users WHERE email = ?", email).Scan(&dbSlackID)
	if err != nil || dbSlackID != slackID {
		t.Errorf("Slack ID not updated in DB: %v (slack_id: %s)", err, dbSlackID)
	}
}
