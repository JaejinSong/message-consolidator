package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestSearchActiveMessages_NoResults(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	results, err := SearchActiveMessages(context.Background(), "nobody@example.com", "hello")
	if err != nil {
		t.Fatalf("SearchActiveMessages: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
