package store

import (
	"context"
	"testing"
)

func TestSQLFix_BatchOperations(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "test@example.com"

	// 1. Test GetTaskTranslationsBatch
	t.Run("GetTaskTranslationsBatch", func(t *testing.T) {
		ids := []int{1, 2, 3}
		_, err := GetTaskTranslationsBatch(ids, "ko")
		if err != nil {
			t.Errorf("GetTaskTranslationsBatch failed: %v", err)
		}
	})

	// 2. Test GetMessagesByIDs
	t.Run("GetMessagesByIDs", func(t *testing.T) {
		ids := []int{1, 2, 3}
		_, err := GetMessagesByIDs(ctx, ids)
		if err != nil {
			t.Errorf("GetMessagesByIDs failed: %v", err)
		}
	})

	// 3. Test DeleteMessages
	t.Run("DeleteMessages", func(t *testing.T) {
		ids := []int{1, 2, 3}
		err := DeleteMessages(email, ids)
		if err != nil {
			t.Errorf("DeleteMessages failed: %v", err)
		}
	})

	// 4. Test HardDeleteMessages
	t.Run("HardDeleteMessages", func(t *testing.T) {
		ids := []int{1, 2, 3}
		err := HardDeleteMessages(email, ids)
		if err != nil {
			t.Errorf("HardDeleteMessages failed: %v", err)
		}
	})

	// 5. Test RestoreMessages
	t.Run("RestoreMessages", func(t *testing.T) {
		ids := []int{1, 2, 3}
		err := RestoreMessages(email, ids)
		if err != nil {
			t.Errorf("RestoreMessages failed: %v", err)
		}
	})
}
