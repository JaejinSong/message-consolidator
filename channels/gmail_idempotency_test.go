package channels

import (
	"context"
	"message-consolidator/config"
	"message-consolidator/store"
	"testing"
)

func TestGmailIdempotency(t *testing.T) {
	// 1. Setup - Fully reset memory and file state
	store.ResetForTest()

	// Why: Use in-memory SQLite with shared cache to ensure multiple connections/tx see the same data,
	// while completely eliminating disk side-effects like .db-shm files.
	dbURL := "file:memdb_gmail_idempotency?mode=memory&cache=shared"
	store.InitDB(context.Background(), &config.Config{TursoURL: dbURL})
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
