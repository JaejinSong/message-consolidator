package store

import (
	"message-consolidator/internal/testutil"
	"context"
	"testing"
)

func TestNormalizeWithCategory(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	tenantEmail := testutil.RandomEmail("alias")

	metadataMu.Lock()
	userCache["jjsong-norm@whatap.io"] = &User{ID: 1, Email: "jjsong-norm@whatap.io", Name: "Jaejin Song"}
	metadataMu.Unlock()

	// 1. Create Contacts properly via AddContact to populate DB + Cache + DSU
	c1, _ := AddContact(ctx, tenantEmail, "hady-norm@whatap.io", "Hady", "Hady Tandibali", "all")
	_, _ = AddContact(ctx, tenantEmail, "ryan-norm@gmail.com", "Ryan", "", "all")
	c3, _ := AddContact(ctx, tenantEmail, "jjsong-norm@whatap.io", "Jaejin Song", "JJ", "all")
	
	// Seed extra aliases for complex resolution tests
	_ = RegisterAlias(ctx, c3, "name", "SongV2", "manual", 5)

	// Why: Validate IDs to satisfy linter and ensure setup is correct.
	if c1 == 0 || c3 == 0 {
		t.Fatalf("Failed to seed contacts")
	}

	tests := []struct {
		testName     string
		input        string
		expectedID   string
		expectedName string
		expectCat    string
	}{
		{"Direct Email Internal", "jjsong-norm@whatap.io", "jjsong-norm@whatap.io", "Jaejin Song", "Internal"},
		{"Name Matching Internal", "Jaejin Song", "jjsong-norm@whatap.io", "Jaejin Song", "Internal"},
		{"Alias Matching Internal", "JJ", "jjsong-norm@whatap.io", "Jaejin Song", "Internal"},
		{"TrimSpace and Domain Priority", " SongV2 ", "jjsong-norm@whatap.io", "Jaejin Song", "Internal"},
		{"Contacts DisplayName Match", "Hady", "hady-norm@whatap.io", "Hady", "Internal"},
		{"Contacts Alias Match", "Hady Tandibali", "hady-norm@whatap.io", "Hady", "Internal"},
		{"Contacts Email Match", "hady-norm@whatap.io", "hady-norm@whatap.io", "Hady", "Internal"},
		{"External Name Match", "Ryan", "ryan-norm@gmail.com", "Ryan", "External"},
		{"External Email", "hady-norm@gmail.com", "hady-norm@gmail.com", "hady-norm@gmail.com", "External"},
		{"External Unknown", "Ryan Unknown", "ryan unknown", "Ryan Unknown", "External"},
		{"Already Categorized Internal", "Jaejin Song (Internal)", "jjsong-norm@whatap.io", "Jaejin Song", "Internal"},
		{"Already Categorized External", "Ryan (External)", "ryan-norm@gmail.com", "Ryan", "External"},
		{"Empty Name", "", "", "", "External"},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			id, name, cat := NormalizeWithCategory(tenantEmail, tt.input)
			if id != tt.expectedID {
				t.Errorf("%s: id = %v, want %v", tt.testName, id, tt.expectedID)
			}
			if name != tt.expectedName {
				t.Errorf("%s: name = %v, want %v", tt.testName, name, tt.expectedName)
			}
			if cat != tt.expectCat {
				t.Errorf("%s: cat = %v, want %v", tt.testName, cat, tt.expectCat)
			}
		})
	}
}
