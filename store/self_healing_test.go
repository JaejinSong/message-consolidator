package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"strings"
	"testing"
)

func TestGetContactByIdentifier(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := testutil.RandomEmail("healing-tenant")
	email := testutil.RandomEmail("healing-user")
	name := "User One"

	// 1. 초기 데이터 삽입
	err = AddContactMapping(context.Background(), tenant, email, name, "", "test")
	if err != nil {
		t.Fatalf("Failed to add contact mapping: %v", err)
	}

	t.Run("LookupByEmail", func(t *testing.T) {
		c, err := GetContactByIdentifier(context.Background(), tenant, email)
		if err != nil || c == nil || c.CanonicalID != email {
			t.Errorf("Lookup by email failed: %v, result: %+v", err, c)
		}
	})

	t.Run("LookupByDisplayName", func(t *testing.T) {
		c, err := GetContactByIdentifier(context.Background(), tenant, name)
		if err != nil || c == nil || c.CanonicalID != email {
			t.Errorf("Lookup by name failed: %v, result: %+v", err, c)
		}
	})

	t.Run("CaseInsensitiveEmail", func(t *testing.T) {
		c, err := GetContactByIdentifier(context.Background(), tenant, strings.ToUpper(email))
		if err != nil || c == nil || c.CanonicalID != email {
			t.Errorf("Case-insensitive lookup by email failed: %v", err)
		}
	})
}
