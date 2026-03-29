package store

import (
	"testing"
)

func TestNormalizeWithCategory(t *testing.T) {
	ResetForTest()

	tenantEmail := "admin@whatap.io"

	metadataMu.Lock()
	userCache["jjsong@whatap.io"] = &User{ID: 1, Email: "jjsong@whatap.io", Name: "Jaejin Song"}
	
	// Mock contacts mapping (Priority 1)
	contactsCache[tenantEmail] = []ContactRecord{
		{
			TenantEmail: tenantEmail,
			CanonicalID: "hady@whatap.io",
			DisplayName: "Hady",
			Aliases:     "Hady Tandibali",
			Source:      "all",
		},
		{
			TenantEmail: tenantEmail,
			CanonicalID: "ryan@gmail.com",
			DisplayName: "Ryan",
			Aliases:     "Ryan S",
			Source:      "all",
		},
		{
			TenantEmail: tenantEmail,
			CanonicalID: "jjsong@whatap.io",
			DisplayName: "Jaejin Song",
			Aliases:     "JJ, Jaejin, Song, SongV2",
			Source:      "all",
		},
	}
	metadataMu.Unlock()

	tests := []struct {
		testName     string
		input        string
		expectedID   string
		expectedName string
		expectCat    string
	}{
		{"Direct Email Internal", "jjsong@whatap.io", "jjsong@whatap.io", "Jaejin Song", "Internal"},
		{"Name Matching Internal", "Jaejin Song", "jjsong@whatap.io", "Jaejin Song", "Internal"},
		{"Alias Matching Internal", "JJ", "jjsong@whatap.io", "Jaejin Song", "Internal"},
		{"TrimSpace and Domain Priority", " SongV2 ", "jjsong@whatap.io", "Jaejin Song", "Internal"},

		// Priority 1: Contacts Table
		{"Contacts DisplayName Match", "Hady", "hady@whatap.io", "Hady", "Internal"},
		{"Contacts Alias Match", "Hady Tandibali", "hady@whatap.io", "Hady", "Internal"},
		{"Contacts Email Match", "hady@whatap.io", "hady@whatap.io", "Hady", "Internal"},

		{"External Name Match", "Ryan", "ryan@gmail.com", "Ryan", "External"},
		{"External Email", "hady@gmail.com", "hady@gmail.com", "hady@gmail.com", "External"},
		{"External Unknown", "Ryan Unknown", "ryan unknown", "Ryan Unknown", "External"},
		{"Already Categorized Internal", "Jaejin Song (Internal)", "jjsong@whatap.io", "Jaejin Song", "Internal"},
		{"Already Categorized External", "Ryan (External)", "ryan@gmail.com", "Ryan", "External"},
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
