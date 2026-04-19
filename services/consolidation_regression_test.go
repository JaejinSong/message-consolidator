package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

// TestTaskConsolidationRegression verifies that a complex multi-paragraph email
// results in exactly ONE task with multiple subtasks, as per v1.1.0 prompt rules.
func TestTaskConsolidationRegression(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "user@example.com"
	
	// Realistic complex email body (Hyundai Transys scenario)
	complexBody := `
Updates
Solution: The attached script and implementation guide will automate the WhaTap service restart process.
Support: I can support the configuration via a remote session.
Follow-up Meeting
Date: April 20th 10:00 AM
[Hyundai Transys] Follow-up: WhaTap Monitoring Service Automation
	`

	// 1. Mock AI Result showing CONSOLIDATED output
	// This simulates how AI should respond based on v1.1.0 prompt
	consolidatedResult := []store.TodoItem{
		{
			Task: "WhaTap 서비스 재시작 프로세스 자동화 구현 및 4월 20일 원격지원",
			State: "new",
			Subtasks: []store.TodoSubtask{
				{Task: "자동화 스크립트 및 가이드 적용"},
				{Task: "원격 세션을 통한 구성 지원"},
				{Task: "4월 20일 팔로업 미팅 진행"},
			},
		},
	}

	mockAI := &RegressionMockAI{
		Results: map[int][]store.TodoItem{
			1: consolidatedResult,
		},
	}
	
	tsrv := &TasksService{}
	svc := NewCompletionService(mockAI, &DefaultTaskStore{}, tsrv, store.GetDB())

	t.Run("ExtractOneTaskWithMultipleSubtasks", func(t *testing.T) {
		mockAI.CurrentTurn = 1
		
		// Simulate saving and processing the message
		_, msgID, _ := store.SaveMessage(ctx, store.GetDB(), store.ConsolidatedMessage{
			UserEmail:    email,
			Source:       "gmail",
			Requester:    "Sender",
			OriginalText: complexBody,
			SourceTS:     "ts_1",
			ThreadID:     "gmail_thread_123", // Why: Required for ProcessPotentialCompletion to bypass guard clause
		})

		msg, _ := store.GetMessageByID(ctx, store.GetDB(), email, msgID)
		
		// Process with AI
		svc.ProcessPotentialCompletion(ctx, msg)

		// Verify result in DB
		updated, _ := store.GetMessageByID(ctx, store.GetDB(), email, msgID)

		if updated.Task != consolidatedResult[0].Task {
			t.Errorf("Expected task title %q, got %q", consolidatedResult[0].Task, updated.Task)
		}

		// Verify subtasks were persisted
		if len(updated.Subtasks) != 3 {
			t.Fatalf("Expected 3 subtasks, got %d", len(updated.Subtasks))
		}

		for i, st := range updated.Subtasks {
			if st.Task != consolidatedResult[0].Subtasks[i].Task {
				t.Errorf("Subtask %d match failed: got %q, want %q", i, st.Task, consolidatedResult[0].Subtasks[i].Task)
			}
		}
	})
}
