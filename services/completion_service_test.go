package services

import (
	"context"
	"message-consolidator/store"
	"testing"
)

// MockAI simulates the AI response for testing
type MockAI struct {
	Complete bool
	BatchIDs []int
	Err      error
}

func (m *MockAI) DoesReplyCompleteTask(ctx context.Context, email, taskText, replyText string) (bool, error) {
	return m.Complete, m.Err
}

func (m *MockAI) CheckTasksBatch(ctx context.Context, email, replyText string, tasks []store.ConsolidatedMessage) ([]int, error) {
	return m.BatchIDs, m.Err
}

// MockStore captures calls to MarkMessageDone
type MockStore struct {
	CapturedIDs []int
	Tasks       []store.ConsolidatedMessage
}

func (m *MockStore) GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return m.Tasks, nil
}

func (m *MockStore) MarkMessageDone(email string, id int, isDone bool) error {
	m.CapturedIDs = append(m.CapturedIDs, id)
	return nil
}

func TestCompletionService_ProcessPotentialCompletion(t *testing.T) {
	ctx := context.Background()

	t.Run("Positive Path - Individual Completion", func(t *testing.T) {
		mockAI := &MockAI{Complete: true}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 101, SourceTS: "original_ts", OriginalText: "Send report"}},
		}
		svc := NewCompletionService(mockAI, mockStore)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			ThreadID:     "thread_1",
			SourceTS:     "reply_ts",
			OriginalText: "I've sent it.",
		}

		svc.ProcessPotentialCompletion(ctx, msg)

		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 101 {
			t.Errorf("Expected task 101 to be marked done, got %v", mockStore.CapturedIDs)
		}
	})

	t.Run("Positive Path - Batch Completion", func(t *testing.T) {
		mockAI := &MockAI{BatchIDs: []int{201, 203}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{
				{ID: 201, OriginalText: "T1"},
				{ID: 202, OriginalText: "T2"},
				{ID: 203, OriginalText: "T3"},
			},
		}
		svc := NewCompletionService(mockAI, mockStore)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			ThreadID:     "thread_batch",
			SourceTS:     "reply_ts",
			OriginalText: "Done with 1 and 3",
		}

		svc.ProcessPotentialCompletion(ctx, msg)

		if len(mockStore.CapturedIDs) != 2 {
			t.Errorf("Expected 2 tasks to be marked done, got %d", len(mockStore.CapturedIDs))
		}
	})

	t.Run("Mention Exclusion", func(t *testing.T) {
		mockAI := &MockAI{Complete: true}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 301, OriginalText: "T1"}},
		}
		svc := NewCompletionService(mockAI, mockStore)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			ThreadID:     "thread_mention",
			OriginalText: "Check this <@U123>",
		}

		svc.ProcessPotentialCompletion(ctx, msg)

		if len(mockStore.CapturedIDs) > 0 {
			t.Error("Expected no tasks to be completed when mention is present")
		}
	})
}


