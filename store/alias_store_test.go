package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"strings"
	"testing"
)

// State Isolation: Reset caches and DB before each test to ensure zero side effects.
func setupAliasTest(t *testing.T) (string, func()) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	
	// Ensure caches are explicitly empty before starting
	ResetForTest()
	
	return "admin@whatap.io", cleanup
}

func TestNormalizeWithCategory(t *testing.T) {
	tenantEmail, cleanup := setupAliasTest(t)
	defer cleanup()
	ctx := context.Background()

	// 1. Setup Mock System Users in Cache
	metadataMu.Lock()
	userCache["jjsong@whatap.io"] = &User{ID: 101, Email: "jjsong@whatap.io", Name: "Jaejin Song"}
	metadataMu.Unlock()

	// 2. Setup Identities for Single Identity Resolution (Jaejin Song)
	jjsongID, _ := AddContact(ctx, tenantEmail, "jjsong@whatap.io", "Jaejin Song", "", "all")
	_ = RegisterAlias(ctx, jjsongID, ContactTypeName, "JJ", "manual", 5)
	_ = UpdateContactType(ctx, jjsongID, CategoryInternal)

	// 3. Setup Ambiguity Scenario (Multiple CanonicalIDs for same name "Min")
	min1ID, _ := AddContact(ctx, tenantEmail, "min1@whatap.io", "Min A", "Min", "manual")
	min2ID, _ := AddContact(ctx, tenantEmail, "min2@external.com", "Min B", "Min", "manual")
	_ = UpdateContactType(ctx, min1ID, CategoryInternal)
	_ = UpdateContactType(ctx, min2ID, CategoryNone) // External

	// 4. Setup Partner Category
	partnerID, _ := AddContact(ctx, tenantEmail, "partner@global.com", "Global Partner", "", "all")
	_ = UpdateContactType(ctx, partnerID, CategoryPartner)

	testCases := []struct {
		testName     string
		input        string
		expectedID   string
		expectedName string
		expectedCat  string
	}{
		{
			testName:     "Tag Removal - Internal Label",
			input:        "Jaejin Song (Internal)",
			expectedID:   "jjsong@whatap.io",
			expectedName: "Jaejin Song",
			expectedCat:  "Internal",
		},
		{
			testName:     "Tag Removal - External Label",
			input:        "Stranger (External)",
			expectedID:   "stranger",
			expectedName: "Stranger",
			expectedCat:  "External",
		},
		{
			testName:     "Case Insensitivity Matching",
			input:        "JJSONG@WHATAP.io",
			expectedID:   "jjsong@whatap.io",
			expectedName: "Jaejin Song",
			expectedCat:  "Internal",
		},
		{
			testName:     "Single Identity Resolution (Alias lookup)",
			input:        "JJ",
			expectedID:   "jjsong@whatap.io",
			expectedName: "Jaejin Song",
			expectedCat:  "Internal",
		},
		{
			testName:     "Ambiguity Safeguard (Duplicate names demote to External)",
			input:        "Min",
			expectedID:   "min",
			expectedName: "Min",
			expectedCat:  "External",
		},
		{
			testName:     "Domain Priority (whatap.io is always Internal)",
			input:        "unknown@whatap.io",
			expectedID:   "unknown@whatap.io",
			expectedName: "unknown@whatap.io",
			expectedCat:  "Internal",
		},
		{
			testName:     "Partner Categorization",
			input:        "Global Partner",
			expectedID:   "partner@global.com",
			expectedName: "Global Partner",
			expectedCat:  "Partner",
		},
		{
			testName:     "Empty Input Handling",
			input:        "",
			expectedID:   "",
			expectedName: "",
			expectedCat:  "External",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testName, func(t *testing.T) {
			id, name, cat := NormalizeWithCategory(tenantEmail, tc.input)

			if id != tc.expectedID {
				t.Errorf("[%s] ID mismatch: got %q, want %q", tc.testName, id, tc.expectedID)
			}
			if name != tc.expectedName {
				t.Errorf("[%s] Name mismatch: got %q, want %q", tc.testName, name, tc.expectedName)
			}
			if cat != tc.expectedCat {
				t.Errorf("[%s] Category mismatch: got %q, want %q", tc.testName, cat, tc.expectedCat)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	tenantEmail, cleanup := setupAliasTest(t)
	defer cleanup()

	// Seed user cache for "me" resolution
	metadataMu.Lock()
	userCache[strings.ToLower(tenantEmail)] = &User{ID: 1, Email: tenantEmail, Name: "Administrator"}
	metadataMu.Unlock()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Current User Logic (me)", "me", "Administrator"},
		{"System Constant (__current_user__)", "__current_user__", "Administrator"},
		{"Unknown Name Pass-through", "Anonymous User", "Anonymous User"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeName(tenantEmail, tt.input)
			if got != tt.expected {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.expected)
			}
		})
	}
}
