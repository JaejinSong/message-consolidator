package channels

import (
	"context"
	"message-consolidator/config"
	"message-consolidator/store"
	"os"
	"testing"
)

func TestGmailIdempotency(t *testing.T) {
	// 1. Setup - Fully reset memory and file state
	store.ResetForTest()
	dbPath := "./test.db"
	_ = os.Remove(dbPath)
	_ = os.Remove(dbPath + "-journal")
	_ = os.Remove(dbPath + "-wal")

	dbURL := "file:./test.db?_busy_timeout=5000"
	store.InitDB(&config.Config{TursoURL: dbURL})
	store.InitContactsTable(nil)

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
