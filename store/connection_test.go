package store

import (
	"testing"
)

func TestDeleteGmailToken(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("failed to setup test db: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	token := "ya29.test-token"

	//Why: Verifies that a Gmail token can be successfully persisted to both the database and the in-memory cache.
	if err := SaveGmailToken(email, token); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	//Why: Confirms that the token is immediately available in the cache after saving to avoid unnecessary database lookups.
	has := HasGmailToken(email)
	if !has {
		t.Errorf("token should exist after save in cache")
	}

	//Why: Tests the explicit removal of a user's Gmail token from the system.
	if err := DeleteGmailToken(email); err != nil {
		t.Fatalf("failed to delete token: %v", err)
	}

	//Why: Validates that the token is properly purged from the cache and database to maintain security and consistency.
	has = HasGmailToken(email)
	if has {
		t.Errorf("token should not exist after delete in cache")
	}
}
