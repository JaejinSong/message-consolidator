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

// MockStore captures calls to MarkMessageDone and UpdateMessageCategory
type MockStore struct {
	CapturedIDs        []int
	ReleasedIDs        []int
	ReleasedCategories []string
	Tasks              []store.ConsolidatedMessage
}

func (m *MockStore) GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return m.Tasks, nil
}

func (m *MockStore) MarkMessageDone(email string, id int, isDone bool) error {
	m.CapturedIDs = append(m.CapturedIDs, id)
	return nil
}

func (m *MockStore) UpdateMessageCategory(email string, id int, category string) error {
	m.ReleasedIDs = append(m.ReleasedIDs, id)
	m.ReleasedCategories = append(m.ReleasedCategories, category)
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

	t.Run("Gmail Header - Should NOT Skip Completion if @ is in Header", func(t *testing.T) {
		mockAI := &MockAI{Complete: true}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 401, SourceTS: "t1", OriginalText: "Send doc"}},
		}
		svc := NewCompletionService(mockAI, mockStore)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			Source:       "gmail",
			ThreadID:     "thread_gmail",
			OriginalText: "T: sender@test.com\nC:\nS: RE: doc\nB:\nDone",
		}

		svc.ProcessPotentialCompletion(ctx, msg)

		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 401 {
			t.Errorf("Expected Gmail reply to complete task despite @ in headers, got %v", mockStore.CapturedIDs)
		}
	})

	t.Run("Mention in Body - Should Still Skip Completion", func(t *testing.T) {
		mockAI := &MockAI{Complete: true}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 501, OriginalText: "T1"}},
		}
		svc := NewCompletionService(mockAI, mockStore)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			Source:       "gmail",
			ThreadID:     "thread_mention",
			OriginalText: "T: s@t.com\nB:\nCheck this @someone",
		}

		svc.ProcessPotentialCompletion(ctx, msg)

		if len(mockStore.CapturedIDs) > 0 {
			t.Error("Expected no completion when mention is in the body")
		}
	})

	t.Run("Two-Phase - Auto-release Waiting Status", func(t *testing.T) {
		mockAI := &MockAI{Complete: false} // Not done, but should release waiting
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 601, Category: "waiting", OriginalText: "Ping me"}},
		}
		svc := NewCompletionService(mockAI, mockStore)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			ThreadID:     "thread_wait",
			OriginalText: "I am checking it",
		}

		svc.ProcessPotentialCompletion(ctx, msg)

		if len(mockStore.ReleasedIDs) != 1 || mockStore.ReleasedIDs[0] != 601 {
			t.Errorf("Expected task 601 to be released, got %v", mockStore.ReleasedIDs)
		}
		if mockStore.ReleasedCategories[0] != "others" {
			t.Errorf("Expected released category to be others, got %s", mockStore.ReleasedCategories[0])
		}
	})
}
