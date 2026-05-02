package store

import (
	"context"
	"database/sql"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestOrderedPair(t *testing.T) {
	t.Parallel()
	a, b := orderedPair(5, 3)
	if a != 3 || b != 5 {
		t.Errorf("orderedPair(5,3) = (%d,%d), want (3,5)", a, b)
	}
	a, b = orderedPair(1, 9)
	if a != 1 || b != 9 {
		t.Errorf("orderedPair(1,9) = (%d,%d), want (1,9)", a, b)
	}
	a, b = orderedPair(7, 7)
	if a != 7 || b != 7 {
		t.Errorf("orderedPair(7,7) = (%d,%d), want (7,7)", a, b)
	}
}

func TestPendingPairContactIDs_Basic(t *testing.T) {
	t.Parallel()
	group := []tokenContact{{1, "A"}, {2, "B"}, {3, "C"}}
	handled := map[[2]int64]bool{}
	ids := pendingPairContactIDs(group, handled)
	if len(ids) < 2 {
		t.Errorf("expected at least 2 ids, got %v", ids)
	}
}

func TestPendingPairContactIDs_AllHandled(t *testing.T) {
	t.Parallel()
	group := []tokenContact{{1, "A"}, {2, "B"}}
	handled := map[[2]int64]bool{{1, 2}: true}
	ids := pendingPairContactIDs(group, handled)
	if len(ids) != 0 {
		t.Errorf("all pairs handled, expected empty ids, got %v", ids)
	}
}

func TestPendingPairContactIDs_Empty(t *testing.T) {
	t.Parallel()
	ids := pendingPairContactIDs([]tokenContact{}, map[[2]int64]bool{})
	if ids != nil {
		t.Errorf("empty group, expected nil ids, got %v", ids)
	}
}

func TestIndexContactsByTokenKey(t *testing.T) {
	t.Parallel()
	contacts := []ContactRecord{
		{ID: 1, DisplayName: "Kim Jinro"},
		{ID: 2, DisplayName: "Jinro Kim"},
		{ID: 3, DisplayName: "Single"},
		{ID: 4, DisplayName: "Linked", MasterContactID: sql.NullInt64{Int64: 1, Valid: true}},
	}
	groups := indexContactsByTokenKey(contacts)
	// "kim jinro" and "jinro kim" normalize to the same key
	if len(groups) == 0 {
		t.Error("expected at least one group")
	}
	// linked contact should be skipped
	for _, tc := range groups {
		for _, c := range tc {
			if c.id == 4 {
				t.Error("linked contact should be excluded")
			}
		}
	}
}

func TestProposalsFromTokenGroups_Empty(t *testing.T) {
	t.Parallel()
	proposals := proposalsFromTokenGroups(map[string][]tokenContact{}, map[[2]int64]bool{})
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(proposals))
	}
}

func TestProposalsFromTokenGroups_SingleContact(t *testing.T) {
	t.Parallel()
	groups := map[string][]tokenContact{"key": {{1, "A"}}}
	proposals := proposalsFromTokenGroups(groups, map[[2]int64]bool{})
	if len(proposals) != 0 {
		t.Errorf("single contact group should yield 0 proposals, got %d", len(proposals))
	}
}

func TestAmbiguousIdentityError(t *testing.T) {
	t.Parallel()
	err := &AmbiguousIdentityError{Identifier: "foo", Emails: []string{"a@b.com"}}
	if err.Error() == "" {
		t.Error("Error() should not be empty")
	}
	if err.Identifier != "foo" {
		t.Errorf("Identifier = %q, want foo", err.Identifier)
	}
}

func TestNewGroupID(t *testing.T) {
	t.Parallel()
	a := NewGroupID()
	b := NewGroupID()
	if len(a) != 16 {
		t.Errorf("NewGroupID len = %d, want 16", len(a))
	}
	if a == b {
		t.Error("NewGroupID should produce unique values")
	}
}

func TestGetCandidateContacts_Empty(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	contacts, err := GetCandidateContacts(context.Background(), "u@example.com")
	if err != nil {
		t.Fatalf("GetCandidateContacts: %v", err)
	}
	if len(contacts) != 0 {
		t.Errorf("expected 0 contacts, got %d", len(contacts))
	}
}
