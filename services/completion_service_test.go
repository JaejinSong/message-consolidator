package services

import (
	"context"
	"database/sql"
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"
)

// MockAI simulates the AI response for testing
type MockAI struct {
	Results []store.TodoItem
	Err     error
}

func (m *MockAI) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	return m.Results, m.Err
}

// MockStore captures calls to MarkMessageDone and UpdateMessageCategory
type MockStore struct {
	CapturedIDs        []int
	ReleasedIDs        []int
	ReleasedCategories []string
	Tasks              []store.ConsolidatedMessage
}

func (m *MockStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return m.Tasks, nil
}

func (m *MockStore) GetActiveContextTasks(ctx context.Context, q store.Querier, email, source, room string) ([]store.ConsolidatedMessage, error) {
	return m.Tasks, nil
}

func (m *MockStore) MarkMessageDone(ctx context.Context, tx *sql.Tx, email string, id int, isDone bool) error {
	m.CapturedIDs = append(m.CapturedIDs, id)
	return nil
}

func (m *MockStore) UpdateMessageCategory(ctx context.Context, tx *sql.Tx, email string, id int, category string) error {
	m.ReleasedIDs = append(m.ReleasedIDs, id)
	m.ReleasedCategories = append(m.ReleasedCategories, category)
	return nil
}

func (m *MockStore) HandleTaskState(ctx context.Context, tx *sql.Tx, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	if item.State == "resolve" {
		m.CapturedIDs = append(m.CapturedIDs, *item.ID)
	}
	return 0, nil
}

func TestCompletionService_ProcessPotentialCompletion(t *testing.T) {
	ctx := context.Background()
 
	t.Run("Positive Path - Individual Completion", func(t *testing.T) {
		// AI Proposes: Task is completed, but doesn't know ID 101 (outputs 0)
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "resolve", Task: "Send report"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 101, SourceTS: "original_ts", Task: "Send report", OriginalText: "Send report"}},
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
 
	t.Run("Mention in Body - Should Delegate (Update)", func(t *testing.T) {
		// AI Proposes: Delegate to 김개발 (outputs 0)
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "update", Task: "T1", AssignedTo: "김개발"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 501, Task: "T1", OriginalText: "T1"}},
		}
		svc := NewCompletionService(mockAI, mockStore)
 
		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			Source:       "slack",
			ThreadID:     "thread_mention",
			OriginalText: "이거 확인해주세요 @김개발",
		}
 
		svc.ProcessPotentialCompletion(ctx, msg)
	})
}
