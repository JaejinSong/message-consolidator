package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestBulkAliasResolution(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenant := "test@example.com"

	// 1. Test Tenant Identity Bypass
	t.Run("TenantBypass", func(t *testing.T) {
		res, _, _ := GetContactsByIdentifiers(ctx, tenant, []string{tenant})
		if res[tenant] == nil || res[tenant].DisplayName != "Me" {
			t.Errorf("Expected 'Me' for tenant bypass, got %+v", res[tenant])
		}
	})

	// 2. Test Negative Caching
	t.Run("NegativeCaching", func(t *testing.T) {
		unknown := "unknown_id_123"
		// First call should record missing
		_, _, _ = GetContactsByIdentifiers(ctx, tenant, []string{unknown})
		
		metadataMu.RLock()
		mappings := contactsCache[tenant]
		found := false
		for _, m := range mappings {
			if m.CanonicalID == unknown && m.ID == -1 {
				found = true
				break
			}
		}
		metadataMu.RUnlock()
		if !found {
			t.Errorf("Sentinel value ID=-1 not found in cache for %s", unknown)
		}
	})

	// 3. Test Bulk Resolution Mapping
	t.Run("BulkMapping", func(t *testing.T) {
		names := []string{tenant, "unknown_1", "unknown_2"}
		mapping := BulkResolveAliases(ctx, tenant, names)
		
		if mapping[tenant] != "Me" {
			t.Errorf("Expected 'Me', got %s", mapping[tenant])
		}
		if mapping["unknown_1"] != "unknown_1" {
			t.Errorf("Expected original name for unknown, got %s", mapping["unknown_1"])
		}
	})
}

func TestNormalizeIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Jaejin Song", "jaejin song"},
		{"Jaejin Song (jj)", "jaejin song"},
		{"  Test (Aux)  ", "test"},
		{"user@example.com", "user@example.com"},
		{"+82-10-1234", "+82-10-1234"},
	}

	for _, tt := range tests {
		if got := NormalizeIdentifier(tt.input); got != tt.expected {
			t.Errorf("NormalizeIdentifier(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
