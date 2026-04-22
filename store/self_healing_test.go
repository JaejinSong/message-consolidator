package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"strings"
	"testing"
)

// TestUpdateMessageIdentityPreservesUnchangedField guards against the bug where
// nullString("") is {Valid:true}, causing COALESCE to overwrite existing fields with "".
func TestUpdateMessageIdentityPreservesUnchangedField(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("identity-test")
	msg := ConsolidatedMessage{
		UserEmail: email, Source: "whatsapp", Room: "TestRoom",
		Task: "Test task", Requester: "Alice", Assignee: "Bob",
		SourceTS: testutil.RandomEmail("ts"),
	}
	_, id, err := SaveMessage(ctx, GetDB(), msg)
	if err != nil || id == 0 {
		t.Fatalf("SaveMessage: %v", err)
	}

	// Self-healing updates only assignee → requester must be preserved.
	if err := UpdateMessageIdentity(ctx, GetDB(), email, "TestRoom", id, "", "charlie@example.com"); err != nil {
		t.Fatalf("UpdateMessageIdentity: %v", err)
	}

	got, err := GetMessageByID(ctx, GetDB(), email, id)
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if got.Requester != "Alice" {
		t.Errorf("requester should be preserved as 'Alice', got %q", got.Requester)
	}
	if got.Assignee != "charlie@example.com" {
		t.Errorf("assignee should be updated to 'charlie@example.com', got %q", got.Assignee)
	}
}

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
