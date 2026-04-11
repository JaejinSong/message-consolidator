package services

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

type MockMergeAI struct {
	ExpectedTitle string
	Error         error
}

func (m *MockMergeAI) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	if m.Error != nil { return "", m.Error }
	return m.ExpectedTitle, nil
}

func TestMergeTasks_AIFlow(t *testing.T) {
	cleanup, _ := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	defer cleanup()

	email := testutil.RandomEmail("test")
	ctx := context.Background()

	// 1. Create dummy tasks
	_, destID, _ := store.SaveMessage(ctx, store.GetDB(), store.ConsolidatedMessage{
		UserEmail: email, Task: "Old Destination Title", OriginalText: "Old Text", Category: "inbox", SourceTS: testutil.RandomTS("ts-dest"),
	})
	_, srcID, _ := store.SaveMessage(ctx, store.GetDB(), store.ConsolidatedMessage{
		UserEmail: email, Task: "Source Title", OriginalText: "Source Text", Category: "inbox", SourceTS: testutil.RandomTS("ts-src"),
	})

	t.Run("Success Case - AI returns new title", func(t *testing.T) {
		mockAI := &MockMergeAI{ExpectedTitle: "AI Generated Action Title"}
		svc := NewTasksService(nil, mockAI)

		err := svc.MergeTasks(ctx, email, []int64{int64(srcID)}, int64(destID))
		if err != nil { t.Fatalf("MergeTasks failed: %v", err) }

		// Verify title updated
		msg, _ := store.GetMessageByID(ctx, store.GetDB(), email, destID)
		if msg.Task != "AI Generated Action Title" {
			t.Errorf("Expected title 'AI Generated Action Title', got %q", msg.Task)
		}

		// Verify source marked as merged
		src, _ := store.GetMessageByID(ctx, store.GetDB(), email, srcID)
		if src.Category != "merged" {
			t.Errorf("Expected source category 'merged', got %q", src.Category)
		}
	})

	t.Run("Fallback Case - AI error uses original title", func(t *testing.T) {
		mockAI := &MockMergeAI{Error: fmt.Errorf("AI timeout")}
		svc := NewTasksService(nil, mockAI)

		// Create new dest for fallback test
		_, destID2, _ := store.SaveMessage(ctx, store.GetDB(), store.ConsolidatedMessage{
			UserEmail: email, Task: "Keep Original", OriginalText: "...", Category: "inbox", SourceTS: testutil.RandomTS("ts-dest"),
		})

		err := svc.MergeTasks(ctx, email, []int64{int64(srcID)}, int64(destID2))
		if err != nil { t.Fatalf("MergeTasks failed: %v", err) }

		msg, _ := store.GetMessageByID(ctx, store.GetDB(), email, destID2)
		if msg.Task != "Keep Original" {
			t.Errorf("Expected fallback to original title 'Keep Original', got %q", msg.Task)
		}
	})
}
