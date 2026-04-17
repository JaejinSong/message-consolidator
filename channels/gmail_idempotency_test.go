package channels

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

func TestGmailIdempotency(t *testing.T) {
	// Why: Use testutil.SetupTestDB to avoid broken cache=shared in-memory DSN.
	// modernc.org/sqlite ignores cache=shared, causing each pool connection to see an empty DB.
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()
	store.InitContactsTable(context.Background(), store.GetDB())

	ctx := context.Background()
	email := "test@example.com"
	msgID := "gmail-test-123"

	// 2. Initial state: Not processed
	processed, _ := store.IsProcessed(ctx, store.GetDB(), email, msgID)
	if processed {
		t.Fatalf("Expected processed to be false for new ID")
	}

	// 3. Mark as processed (by saving a message with this source_ts)
	msg := store.ConsolidatedMessage{
		UserEmail: email,
		Source:    "gmail",
		SourceTS:  msgID,
		Task:      "Test Task",
	}
	success, _, err := store.SaveMessage(ctx, store.GetDB(), msg)
	if err != nil {
		t.Fatalf("Failed to save message: %v", err)
	}
	if !success {
		t.Fatalf("Expected first save to succeed")
	}

	// 4. Verify processed
	processed, _ = store.IsProcessed(ctx, store.GetDB(), email, msgID)
	if !processed {
		t.Errorf("Expected processed to be true after saving")
	}

	// 5. Test DB-level idempotency
	// Calling SaveMessage again with same ID should return success=false, id=0 and no error.
	success, id, err := store.SaveMessage(ctx, store.GetDB(), msg)
	if err != nil {
		t.Errorf("SaveMessage should not error on conflict: %v", err)
	}
	if success {
		t.Errorf("Expected success=false on second save, got true")
	}
	if id != 0 {
		t.Errorf("Expected id=0 on conflict, got %d", id)
	}
}
