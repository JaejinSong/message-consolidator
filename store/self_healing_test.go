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
	alias := testutil.RandomID("alias")

	// 1. 초기 데이터 삽입
	err = AddContactMapping(context.Background(), tenant, email, name, alias, "test")
	if err != nil {
		t.Fatalf("Failed to add contact mapping: %v", err)
	}

	// Cache 강제 로드 (GetContactByIdentifier는 내부적으로 EnsureCacheInitialized 호출)
	
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

	t.Run("LookupByAlias", func(t *testing.T) {
		c, err := GetContactByIdentifier(context.Background(), tenant, alias)
		if err != nil || c == nil || c.CanonicalID != email {
			t.Errorf("Lookup by alias failed: %v, result: %+v", err, c)
		}
	})

	t.Run("CaseInsensitiveAlias", func(t *testing.T) {
		c, err := GetContactByIdentifier(context.Background(), tenant, strings.ToLower(alias)) // 소문자 검색
		if err != nil || c == nil || c.CanonicalID != email {
			t.Errorf("Case-insensitive lookup by alias failed: %v", err)
		}
	})
}
