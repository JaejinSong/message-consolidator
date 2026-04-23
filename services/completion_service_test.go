package services

import (
	"context"
	"message-consolidator/ai"
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

func (m *MockAI) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string) (ai.TaskTransition, error) {
	if len(m.Results) > 0 {
		// Normalizing to uppercase to match handleCompletionResult switch cases
		status := "NONE"
		if m.Results[0].State == "resolve" {
			status = "RESOLVE"
		} else if m.Results[0].State == "update" {
			status = "UPDATE"
		} else if m.Results[0].State == "new" {
			status = "NEW"
		}
		return ai.TaskTransition{Status: status, UpdatedText: m.Results[0].Task}, m.Err
	}
	return ai.TaskTransition{Status: "NONE"}, m.Err
}

func (m *MockAI) Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error) {
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

func (m *MockStore) MarkMessageDone(ctx context.Context, q store.Querier, email string, id int, isDone bool) error {
	m.CapturedIDs = append(m.CapturedIDs, id)
	return nil
}

func (m *MockStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id int, category string) error {
	m.ReleasedIDs = append(m.ReleasedIDs, id)
	m.ReleasedCategories = append(m.ReleasedCategories, category)
	return nil
}

func (m *MockStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	if item.State == "resolve" {
		m.CapturedIDs = append(m.CapturedIDs, *item.ID)
	}
	return 0, nil
}

func (m *MockStore) UpdateTaskText(ctx context.Context, q store.Querier, email string, id int, task string) error {
	m.ReleasedIDs = append(m.ReleasedIDs, id)
	return nil
}

func (m *MockStore) GetMessageByID(ctx context.Context, q store.Querier, email string, id int) (store.ConsolidatedMessage, error) {
	return store.ConsolidatedMessage{}, nil
}

func TestCompletionService_ProcessPotentialCompletion(t *testing.T) {
	ctx := context.Background()

	t.Run("Positive Path - Individual Completion", func(t *testing.T) {
		// AI Proposes: Task is completed, but doesn't know ID 101 (outputs 0)
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "resolve", Task: "Send report"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 101, SourceTS: "original_ts", Task: "Send report", OriginalText: "Send report"}},
		}
		tsrv := &TasksService{}
		svc := NewCompletionService(mockAI, mockStore, tsrv, nil)

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

	t.Run("Current User Reply (UPDATE) - Should Reclassify as Delegated", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "update", Task: "IFC 말레이시아 미팅 참여 범위 확정"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 202, Task: "IFC 말레이시아 미팅 참여 범위 확정"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_ifc",
			OriginalText:       "이 부분 다시 확인 부탁드립니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true for current-user reply")
		}
		if len(mockStore.CapturedIDs) != 0 {
			t.Errorf("expected task NOT marked done, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
		if len(mockStore.ReleasedCategories) != 1 || mockStore.ReleasedCategories[0] != CategoryRequested {
			t.Errorf("expected category=%q, got %v", CategoryRequested, mockStore.ReleasedCategories)
		}
	})

	t.Run("Current User Reply (UPDATE+updatedText) - Should update category AND task text", func(t *testing.T) {
		newScope := "JVM Crash/ZFS 블로그 검색 최적화 및 가독성 개선"
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "update", Task: newScope}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 204, Task: "JVM Crash/ZFS 블로그 최신화 및 검수"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_blog",
			OriginalText:       "최신화보다 검색 확률 및 가독성 개선 방향으로 도움드리겠습니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true")
		}
		if len(mockStore.ReleasedCategories) != 1 || mockStore.ReleasedCategories[0] != CategoryRequested {
			t.Errorf("expected category=%q, got %v", CategoryRequested, mockStore.ReleasedCategories)
		}
		// ReleasedIDs collects both UpdateMessageCategory and UpdateTaskText calls.
		if len(mockStore.ReleasedIDs) != 2 {
			t.Errorf("expected category+text both updated (2 ReleasedIDs), got %v", mockStore.ReleasedIDs)
		}
	})

	t.Run("Current User Reply (RESOLVE) - Should Mark Done", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "resolve", Task: "IFC 말레이시아 미팅 참여 범위 확정"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 203, Task: "IFC 말레이시아 미팅 참여 범위 확정"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_ifc_done",
			OriginalText:       "네, 5월 5-6일 참석 확정입니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true")
		}
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 203 {
			t.Errorf("expected task 203 marked done, got %v", mockStore.CapturedIDs)
		}
		if len(mockStore.ReleasedCategories) != 0 {
			t.Errorf("expected no category change on RESOLVE, got %v", mockStore.ReleasedCategories)
		}
	})

	t.Run("Multiple Tasks in Thread - All Should Be Processed", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "resolve", Task: "IFC 미팅"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{
				{ID: 11689, Task: "Andy에게 기술 지원 범위 확인"},
				{ID: 11690, Task: "5월 5-6일 미팅 참여 범위 확정"},
			},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "19db836a225d9092",
			OriginalText: "네, 5월 5-6일 참석 확정입니다.",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true")
		}
		if len(mockStore.CapturedIDs) != 2 {
			t.Errorf("expected both tasks marked done, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("Mention in Body - Should Delegate (Update)", func(t *testing.T) {
		// AI Proposes: Delegate to 김개발 (outputs 0)
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(0), State: "update", Task: "T1", AssignedTo: "김개발"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 501, Task: "T1", OriginalText: "T1"}},
		}
		tsrv := &TasksService{}
		svc := NewCompletionService(mockAI, mockStore, tsrv, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			Source:       "slack",
			ThreadID:     "thread_mention",
			OriginalText: "이거 확인해주세요 @김개발",
		}

		svc.ProcessPotentialCompletion(ctx, msg)
	})
}
