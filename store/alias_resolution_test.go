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

	// 2. Test Unknown Identifier Returns Nil
	t.Run("UnknownIdentifier", func(t *testing.T) {
		unknown := "unknown_id_123"
		res, _, _ := GetContactsByIdentifiers(ctx, tenant, []string{unknown})
		if res[unknown] != nil {
			t.Errorf("Expected nil for unknown identifier, got %+v", res[unknown])
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
		{"-- Ardi --", "ardi"},
		{"~~ test ~~", "test"},
	}

	for _, tt := range tests {
		if got := NormalizeIdentifier(tt.input); got != tt.expected {
			t.Errorf("NormalizeIdentifier(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
