package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestCacheInvalidationAndReadThrough(t *testing.T) {
	// Setup
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("cache-inv")
	msg := ConsolidatedMessage{
		UserEmail: email,
		Source:    "test",
		SourceTS:  "123",
		Task:      "Original Task",
	}

	// 1. Save should invalidate (initialize empty cache if first time, then invalidate)
	_, id, err := SaveMessage(context.Background(), GetDB(), msg)
	if err != nil {
		t.Fatalf("SaveMessage failed: %v", err)
	}

	// Check if initialized (SaveMessage calls InvalidateCache which deletes the key)
	cacheMu.RLock()
	init := cacheInitialized[email]
	cacheMu.RUnlock()
	if init {
		t.Errorf("Cache should be NOT initialized after SaveMessage (invalidated)")
	}

	// 2. GetMessages should trigger Read-Through
	msgs, err := GetMessages(context.Background(), email)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Task != "Original Task" {
		t.Errorf("Unexpected messages: %+v", msgs)
	}

	cacheMu.RLock()
	initAfter := cacheInitialized[email]
	cacheMu.RUnlock()
	if !initAfter {
		t.Errorf("Cache should be initialized after GetMessages")
	}

	// 3. Update should invalidate
	err = MarkMessageDone(context.Background(), GetDB(), email, id, true)
	if err != nil {
		t.Fatalf("MarkMessageDone failed: %v", err)
	}

	cacheMu.RLock()
	initFinal := cacheInitialized[email]
	cacheMu.RUnlock()
	if initFinal {
		t.Errorf("Cache should be invalidated after update")
	}

	// 4. Next GetMessages should load updated state (Read-Through)
	// Why: Now that we removed the 7-day threshold, 'Done' tasks should disappear from Active immediately.
	msgsFinal, err := GetMessages(context.Background(), email)
	if err != nil {
		t.Fatalf("Second GetMessages failed: %v", err)
	}
	if len(msgsFinal) != 0 {
		t.Errorf("Message should have moved to archive immediately, but still in active: %+v", msgsFinal)
	}

	// 5. Verify it's in the Archive
	archived, err := GetArchivedMessages(context.Background(), email)
	if err != nil {
		t.Fatalf("GetArchivedMessages failed: %v", err)
	}
	if len(archived) != 1 || !archived[0].Done {
		t.Errorf("Message should be in archive: %+v", archived)
	}
}
