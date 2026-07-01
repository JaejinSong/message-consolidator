package services

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"
)

// MockAI simulates the AI response for testing
type MockAI struct {
	Results  []store.TodoItem
	Sequence []ai.TaskTransition // popped per EvaluateTaskTransition call; falls back to Results if empty
	Err      error
	CallCount int
}

func (m *MockAI) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	return m.Results, m.Err
}

func (m *MockAI) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (ai.TaskTransition, error) {
	m.CallCount++
	// Why: Sequence-mode allows tests to return different statuses per call for
	// per-task evaluation verification. Falls back to Results for single-status cases.
	if len(m.Sequence) > 0 {
		next := m.Sequence[0]
		m.Sequence = m.Sequence[1:]
		return next, m.Err
	}
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
	CapturedIDs        []store.MessageID
	ReleasedIDs        []store.MessageID
	ReleasedCategories []string
	NewItemTasks       []string
	Tasks              []store.ConsolidatedMessage
	RecentGmailTasks   []store.ConsolidatedMessage
	UpdatedSubtasks    map[store.MessageID][]store.Subtask
	HasAnyTask         bool // controls HasAnyTaskInThread; default false preserves prior guard behavior
	Candidates         map[store.MessageID]store.CompletionCandidate
}

func (m *MockStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return m.Tasks, nil
}

func (m *MockStore) HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error) {
	return m.HasAnyTask, nil
}

func (m *MockStore) GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error) {
	return "", nil
}

func (m *MockStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error {
	m.ReleasedIDs = append(m.ReleasedIDs, id)
	m.ReleasedCategories = append(m.ReleasedCategories, category)
	return nil
}

func (m *MockStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	switch item.State {
	case "resolve":
		m.CapturedIDs = append(m.CapturedIDs, *item.ID)
	case "update":
		m.ReleasedIDs = append(m.ReleasedIDs, *item.ID)
	case "new":
		m.NewItemTasks = append(m.NewItemTasks, item.Task)
	}
	return 0, nil
}

func (m *MockStore) GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error) {
	return m.RecentGmailTasks, nil
}

func (m *MockStore) AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error {
	if m.Candidates == nil {
		m.Candidates = make(map[store.MessageID]store.CompletionCandidate)
	}
	m.Candidates[id] = cand
	return nil
}

func (m *MockStore) UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error {
	if m.UpdatedSubtasks == nil {
		m.UpdatedSubtasks = make(map[store.MessageID][]store.Subtask)
	}
	m.UpdatedSubtasks[id] = subtasks
	return nil
}

func TestCompletionService_ProcessPotentialCompletion(t *testing.T) {
	ctx := context.Background()

	t.Run("Positive Path - Individual Completion", func(t *testing.T) {
		// AI Proposes: Task is completed, but doesn't know ID 101 (outputs 0)
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "Send report"}}}
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

	t.Run("Current User Reply (UPDATE) - Substantive - AI Called - Task Updated", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "update", Task: "IFC 말레이시아 미팅 참여 범위 확정"}}}
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
		// Substantive text reaches AI; UPDATE response calls HandleTaskState("update") → ReleasedIDs
		if mockAI.CallCount == 0 {
			t.Error("expected AI to be called for substantive fromMe reply")
		}
		if len(mockStore.CapturedIDs) != 0 {
			t.Errorf("expected task NOT marked done, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
		if len(mockStore.ReleasedIDs) != 1 {
			t.Errorf("expected task update via HandleTaskState, got ReleasedIDs=%v", mockStore.ReleasedIDs)
		}
	})

	t.Run("Current User Reply (UPDATE+updatedText) - AI Called - Task Text Updated", func(t *testing.T) {
		newScope := "JVM Crash/ZFS 블로그 검색 최적화 및 가독성 개선"
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "update", Task: newScope}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 205, Task: "JVM Crash/ZFS 블로그 최신화 및 검수"}},
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
		// Substantive text: AI is called, UPDATE fires HandleTaskState → ReleasedIDs
		if mockAI.CallCount == 0 {
			t.Error("expected AI to be called for substantive fromMe reply")
		}
		if len(mockStore.ReleasedIDs) != 1 {
			t.Errorf("expected task update via HandleTaskState, got ReleasedIDs=%v", mockStore.ReleasedIDs)
		}
	})

	t.Run("Current User ACK-only Reply - Reclassifies, Never Auto-Closes", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "IFC 말레이시아 미팅 참여 범위 확정"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 203, Task: "IFC 말레이시아 미팅 참여 범위 확정"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_ifc_ack",
			OriginalText:       "네, 감사합니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true")
		}
		// AI should NOT have been called (ack-only shortcut fires first)
		if mockAI.CallCount != 0 {
			t.Errorf("expected AI not called for ack-only reply, got CallCount=%d", mockAI.CallCount)
		}
		if len(mockStore.CapturedIDs) != 0 {
			t.Errorf("expected NOT auto-closed, got %v", mockStore.CapturedIDs)
		}
		if len(mockStore.ReleasedCategories) != 1 || mockStore.ReleasedCategories[0] != CategoryRequested {
			t.Errorf("expected category=%q, got %v", CategoryRequested, mockStore.ReleasedCategories)
		}
	})

	t.Run("Current User Substantive Reply (RESOLVE-shaped) - AI Decides Close", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "IFC 말레이시아 미팅 참여 범위 확정"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 204, Task: "IFC 말레이시아 미팅 참여 범위 확정"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_ifc_substantive",
			OriginalText:       "네, 5월 5-6일 참석 확정입니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true")
		}
		// Substantive reply must reach AI and AI RESOLVE must close the task
		if mockAI.CallCount == 0 {
			t.Error("expected AI to be called for substantive fromMe reply")
		}
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 204 {
			t.Errorf("expected task 204 closed by AI RESOLVE, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("Current User Substantive Reply - AI Called - RESOLVE Applied", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "X"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 301, Task: "Provide WhaTap agent error capture documentation"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_apm_doc",
			OriginalText:       "Most APMs, including WhaTap, record error data by hooking into the exception handling logic of frameworks and languages.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true")
		}
		// Substantive content: AI must be called and RESOLVE must close the task
		if mockAI.CallCount == 0 {
			t.Error("expected AI to be called for substantive fromMe reply")
		}
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 301 {
			t.Errorf("expected task 301 closed, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("Current User Reply Redirect-to-Channel - Auto-Resolves", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "Analyze API error"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 305, Task: "Analyze API error message for authentication issues"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_dm_redirect",
			OriginalText:       "에러메시지 분석은 인증정보가 포함되어서 DM 으로 안내 드렸습니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true")
		}
		if mockAI.CallCount == 0 {
			t.Error("expected AI to be called for redirect reply")
		}
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 305 {
			t.Errorf("expected task 305 closed by AI RESOLVE, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("Current User Reply Substitute-Attendance - Auto-Resolves", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "Attend weekly report"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 306, Task: "Attend weekly report meeting as substitute"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_substitute",
			OriginalText:       "주간보고 대타 참석 하겠습니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true")
		}
		if mockAI.CallCount == 0 {
			t.Error("expected AI to be called for delegation acceptance reply")
		}
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 306 {
			t.Errorf("expected task 306 closed by AI RESOLVE, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("Current User Multi-Task Substantive Reply - Per-Task AI Evaluation", func(t *testing.T) {
		// Why: Per-task evaluation must resolve task A and leave task B open when AI
		// returns different statuses for each (Sequence pops one per call).
		mockAI := &MockAI{
			Sequence: []ai.TaskTransition{
				{Status: "RESOLVE", UpdatedText: ""},
				{Status: "NONE", UpdatedText: ""},
			},
		}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{
				{ID: 401, Task: "Attend weekly report as substitute"},
				{ID: 402, Task: "Analyze API authentication error"},
			},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_multi",
			OriginalText:       "네, 5월 5-6일 참석 확정입니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true")
		}
		if mockAI.CallCount != 2 {
			t.Errorf("expected 2 AI calls for 2 tasks, got %d", mockAI.CallCount)
		}
		// Only task 401 resolved; task 402 got NONE
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 401 {
			t.Errorf("expected only task 401 closed, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("Multiple Tasks in Thread - All Should Be Processed", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve", Task: "IFC 미팅"}}}
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
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "update", Task: "T1", AssignedTo: "김개발"}}}
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

	// Why: Regression for token-bleed bug. When the thread had no incomplete parent
	// task, fallbackToNewExtraction used to return void, leaving handled=false. The
	// caller (handleGmailThreadActivity) then skipped MarkAsProcessed AND left the
	// message in filteredMsgs, triggering: (a) a second batch Analyze in the SAME
	// scan cycle, and (b) re-processing every cycle thereafter. Fix is to return
	// true once Analyze succeeds so the message gets marked processed.
	t.Run("No Thread Parent - Fallback returns handled=true to stop token bleed", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{
			{ID: ptr(store.MessageID(0)), State: "new", Task: "Review the launch checklist"},
		}}
		mockStore := &MockStore{Tasks: nil} // no thread parent → fallback path
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "test@example.com",
			Source:       "gmail",
			Room:         "Gmail",
			ThreadID:     "thread_no_parent",
			SourceTS:     "msg_001",
			OriginalText: "Please review the launch checklist by EOD.",
		}

		handled, err := svc.ProcessPotentialCompletion(ctx, msg)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !handled {
			t.Fatal("expected handled=true so caller can MarkAsProcessed and stop re-extraction")
		}
		if len(mockStore.NewItemTasks) != 1 || mockStore.NewItemTasks[0] != "Review the launch checklist" {
			t.Errorf("expected fallback HandleTaskState(new) call, got NewItemTasks=%v", mockStore.NewItemTasks)
		}
	})

	t.Run("No Thread Parent - AI Analyze fails - Fallback returns handled=false", func(t *testing.T) {
		mockAI := &MockAI{Results: nil, Err: nil} // empty items → fallback returns false
		mockStore := &MockStore{Tasks: nil}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail: "test@example.com", Source: "gmail",
			ThreadID: "thread_empty", SourceTS: "msg_002",
			OriginalText: "...",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if handled {
			t.Fatal("expected handled=false when AI returns no items so the standard pipeline can retry")
		}
		if len(mockStore.NewItemTasks) != 0 {
			t.Errorf("expected no HandleTaskState calls when items empty, got %v", mockStore.NewItemTasks)
		}
	})

	t.Run("Current User Reply with No Existing Tasks - Should NOT Analyze and Return handled=true", func(t *testing.T) {
		// Why: When RequesterCanonical == UserEmail and Tasks is empty, shortcut should
		// reclassify and return handled=true WITHOUT calling Analyze, preventing unnecessary
		// token consumption for new task extraction.
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "new", Task: "Hypothetical new task"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{}, // No existing tasks
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_no_tasks",
			OriginalText:       "문제 해결되었습니다.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		if !handled {
			t.Fatal("expected handled=true to skip batch analyze")
		}
		// Verify Analyze was NOT called by checking that no store handlers were invoked
		if len(mockStore.NewItemTasks) != 0 {
			t.Errorf("expected no new tasks created, got %v", mockStore.NewItemTasks)
		}
		if len(mockStore.CapturedIDs) != 0 {
			t.Errorf("expected no tasks marked done, got %v", mockStore.CapturedIDs)
		}
		if len(mockStore.ReleasedIDs) != 0 {
			t.Errorf("expected no task state changes, got %v", mockStore.ReleasedIDs)
		}
	})

	t.Run("Self-Origin Follow-Up On Closed-Task Thread - Should NOT Skip Guard", func(t *testing.T) {
		// Why: previously guard short-circuited any self-origin mail when no INCOMPLETE
		// task existed, blocking legitimate follow-ups on threads where prior tasks
		// were all closed. Now thread-level any-task check must allow these through.
		mockAI := &MockAI{Results: []store.TodoItem{
			{ID: ptr(store.MessageID(0)), State: "new", Task: "Lock two follow-ups before kickoff"},
		}}
		mockStore := &MockStore{
			Tasks:      []store.ConsolidatedMessage{}, // no INCOMPLETE tasks
			HasAnyTask: true,                          // but thread has done tasks
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:          "jjsong@whatap.io",
			ThreadID:           "thread_closed_only",
			Source:             "gmail",
			Room:               "Gmail",
			OriginalText:       "Two follow-ups to lock before kickoff: confirm venue and send agenda.",
			RequesterCanonical: "jjsong@whatap.io",
		}

		// Guard must NOT short-circuit: fallbackToNewExtraction is exercised instead.
		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)

		// handled=true because fallbackToNewExtraction succeeds (AI returned an item).
		if !handled {
			t.Fatal("expected handled=true: fallback extraction should run when thread has prior done tasks")
		}
		// AI must have been called, confirming the self-origin guard did not fire.
		if mockAI.CallCount == 0 && len(mockStore.NewItemTasks) == 0 {
			t.Error("expected fallback extraction to be reached (AI called or new item saved)")
		}
	})

	t.Run("RESOLVE with subtasks - cascade all done", func(t *testing.T) {
		subtasks := []store.Subtask{
			{Task: "Write welcome email", Done: false},
			{Task: "Set up dev environment", Done: false},
		}
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 601, Task: "Prepare onboarding", Subtasks: subtasks}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "thread_subtask_cascade",
			OriginalText: "All done!",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true")
		}
		updated, ok := mockStore.UpdatedSubtasks[601]
		if !ok {
			t.Fatal("expected UpdateSubtasks called for parent 601")
		}
		for i, s := range updated {
			if !s.Done {
				t.Errorf("subtask[%d] expected done=true after RESOLVE cascade, got false", i)
			}
		}
		if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 601 {
			t.Errorf("expected parent 601 resolved, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("RESOLVE no subtasks - no UpdateSubtasks call", func(t *testing.T) {
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 602, Task: "Simple task"}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "thread_no_subtasks",
			OriginalText: "Done.",
		}

		svc.ProcessPotentialCompletion(ctx, msg)
		if len(mockStore.UpdatedSubtasks) != 0 {
			t.Errorf("expected no UpdateSubtasks call for task with no subtasks, got %v", mockStore.UpdatedSubtasks)
		}
	})

	t.Run("UPDATE with subtask_updates - partial subtask update", func(t *testing.T) {
		subtasks := []store.Subtask{
			{Task: "Write welcome email", Done: false},
			{Task: "Set up dev environment", Done: false},
		}
		mockAI := &MockAI{
			Sequence: []ai.TaskTransition{
				{Status: "UPDATE", UpdatedText: "Prepare onboarding materials", SubtaskUpdates: []ai.SubtaskUpdate{{Index: 0, Done: true}}},
			},
		}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 603, Task: "Prepare onboarding", Subtasks: subtasks}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "thread_partial_subtask",
			OriginalText: "Welcome email is done. Dev guide still in progress.",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true for UPDATE")
		}
		updated, ok := mockStore.UpdatedSubtasks[603]
		if !ok {
			t.Fatal("expected UpdateSubtasks called for parent 603")
		}
		if !updated[0].Done {
			t.Error("subtask[0] expected done=true after subtask_updates")
		}
		if updated[1].Done {
			t.Error("subtask[1] expected done=false (not mentioned in subtask_updates)")
		}
		if len(mockStore.CapturedIDs) != 0 {
			t.Errorf("expected parent NOT resolved for UPDATE, got CapturedIDs=%v", mockStore.CapturedIDs)
		}
	})

	t.Run("RESOLVE with 6 subtasks - cascade still applied", func(t *testing.T) {
		// >5 subtasks: AI doesn't receive subtask context, subtask_updates empty,
		// but Path 1 cascade must still mark all done.
		subtasks := make([]store.Subtask, 6)
		for i := range subtasks {
			subtasks[i] = store.Subtask{Task: fmt.Sprintf("Step %d", i+1), Done: false}
		}
		mockAI := &MockAI{Results: []store.TodoItem{{ID: ptr(store.MessageID(0)), State: "resolve"}}}
		mockStore := &MockStore{
			Tasks: []store.ConsolidatedMessage{{ID: 604, Task: "Big onboarding task", Subtasks: subtasks}},
		}
		svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "thread_6subtasks",
			OriginalText: "All tasks completed!",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Fatal("expected handled=true")
		}
		updated, ok := mockStore.UpdatedSubtasks[604]
		if !ok {
			t.Fatal("expected UpdateSubtasks called for parent 604 even with 6 subtasks")
		}
		for i, s := range updated {
			if !s.Done {
				t.Errorf("subtask[%d] expected done=true after cascade, got false", i)
			}
		}
	})
}

func TestCrossThreadSubjectDedup(t *testing.T) {
	ctx := context.Background()

	existingTask := store.ConsolidatedMessage{
		ID:           1,
		Task:         "Exercise 2026 stock options for 2,000 shares by June 5",
		ThreadID:     "threadA",
		Source:       "gmail",
		OriginalText: "T: jjsong@whatap.io\nC: \nS: [안내] 2026년 스톡옵션 행사 안내\nB:\n본문",
	}

	t.Run("cross-thread UPDATE routes to existing task", func(t *testing.T) {
		mockStore := &MockStore{
			Tasks:            []store.ConsolidatedMessage{},
			RecentGmailTasks: []store.ConsolidatedMessage{existingTask},
		}
		mockAI := &MockAI{Results: []store.TodoItem{{State: "update", Task: "Updated stock option task"}}}

		svc := NewCompletionService(mockAI, mockStore, nil, nil)
		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "threadB",
			Source:       "gmail",
			OriginalText: "T: jjsong@whatap.io\nC: \nS: [재안내] 2026년 스톡옵션 행사 안내\nB:\n재안내 본문",
		}

		handled, err := svc.ProcessPotentialCompletion(ctx, msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handled {
			t.Error("expected handled=true for cross-thread UPDATE")
		}
		if len(mockStore.ReleasedIDs) == 0 {
			t.Error("expected UpdateMessageCategory or HandleTaskState update call")
		}
	})

	t.Run("cross-thread NEW falls back to new extraction", func(t *testing.T) {
		mockStore := &MockStore{
			Tasks:            []store.ConsolidatedMessage{},
			RecentGmailTasks: []store.ConsolidatedMessage{existingTask},
		}
		mockAI := &MockAI{Results: []store.TodoItem{{State: "new", Task: "New task from re-notice"}}}

		svc := NewCompletionService(mockAI, mockStore, nil, nil)
		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "threadB",
			Source:       "gmail",
			OriginalText: "T: jjsong@whatap.io\nC: \nS: [재안내] 2026년 스톡옵션 행사 안내\nB:\n재안내 본문",
		}

		handled, err := svc.ProcessPotentialCompletion(ctx, msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !handled {
			t.Error("expected handled=true (fallback new extraction also returns true)")
		}
		if len(mockStore.NewItemTasks) == 0 {
			t.Error("expected fallback new extraction to create a task")
		}
	})

	t.Run("no similar subject falls back without cross-thread check", func(t *testing.T) {
		differentTask := store.ConsolidatedMessage{
			ID:           2,
			Task:         "Completely different task",
			ThreadID:     "threadC",
			Source:       "gmail",
			OriginalText: "T: jjsong@whatap.io\nC: \nS: [안내] 완전히 다른 주제\nB:\n본문",
		}
		mockStore := &MockStore{
			Tasks:            []store.ConsolidatedMessage{},
			RecentGmailTasks: []store.ConsolidatedMessage{differentTask},
		}
		mockAI := &MockAI{Results: []store.TodoItem{{State: "new", Task: "New task"}}}

		svc := NewCompletionService(mockAI, mockStore, nil, nil)
		msg := store.ConsolidatedMessage{
			UserEmail:    "jjsong@whatap.io",
			ThreadID:     "threadB",
			Source:       "gmail",
			OriginalText: "T: jjsong@whatap.io\nC: \nS: [재안내] 2026년 스톡옵션 행사 안내\nB:\n재안내 본문",
		}

		handled, _ := svc.ProcessPotentialCompletion(ctx, msg)
		if !handled {
			t.Error("expected fallback to handle the message")
		}
	})
}

func TestDefaultTaskStore_UpdateMessageCategory(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	d := &DefaultTaskStore{}
	// Message ID 999 does not exist; UpdateMessageCategory uses RunInTx internally which should succeed silently.
	if err := d.UpdateMessageCategory(context.Background(), nil, "u@example.com", store.MessageID(999999), "merged"); err != nil {
		t.Logf("UpdateMessageCategory returned (expected): %v", err)
	}
}

// stubOpenTaskFinder returns preset semantic candidates for CompletionService tests
// without a live embedding client.
type stubOpenTaskFinder struct {
	candidates []OpenTaskCandidate
	err        error
}

func (s *stubOpenTaskFinder) CandidateOpenTasks(ctx context.Context, email, queryText string, k int) ([]OpenTaskCandidate, error) {
	return s.candidates, s.err
}

func TestHasCompletionSignal(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"보고서 작성 완료했습니다", true},
		{"Done with the deployment", true},
		{"이거 언제까지 될까요?", false},
		{"Any update on this?", false},
		{"방금 배포했어요", true},
		{"", false},
	}
	for _, c := range cases {
		if got := hasCompletionSignal(c.text); got != c.want {
			t.Errorf("hasCompletionSignal(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

// Why: cross-channel completion must be confirm-first — a semantic match to an open task
// in another thread records a pending candidate and must NOT auto-close the task.
func TestCrossChannelSemanticConfirmFirst(t *testing.T) {
	ctx := context.Background()

	openTask := store.ConsolidatedMessage{ID: 42, Task: "Deploy the billing service", ThreadID: "threadX"}
	mockStore := &MockStore{Tasks: []store.ConsolidatedMessage{}} // thread has no open tasks → cross-thread path
	mockAI := &MockAI{Sequence: []ai.TaskTransition{{Status: "RESOLVE"}}}
	svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)
	svc.SetEmbedder(&stubOpenTaskFinder{candidates: []OpenTaskCandidate{{Task: openTask, Score: -0.1}}})

	msg := store.ConsolidatedMessage{
		UserEmail:    "jjsong@whatap.io",
		ThreadID:     "threadY", // different thread than the open task
		Source:       "slack",
		OriginalText: "billing service 배포 완료했습니다",
	}

	handled, err := svc.ProcessPotentialCompletion(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Error("expected handled=true for cross-channel completion candidate")
	}
	if _, ok := mockStore.Candidates[42]; !ok {
		t.Error("expected a completion candidate recorded on task 42")
	}
	if len(mockStore.CapturedIDs) != 0 {
		t.Errorf("confirm-first: task must NOT be auto-resolved, got resolved IDs %v", mockStore.CapturedIDs)
	}
	if c := mockStore.Candidates[42]; c.Status != "pending" {
		t.Errorf("candidate status = %q, want pending", c.Status)
	}
}

// Why: without a completion signal, the semantic path must not fire (bounds embedding cost).
func TestCrossChannelNoSignalSkipsSemantic(t *testing.T) {
	ctx := context.Background()

	openTask := store.ConsolidatedMessage{ID: 7, Task: "Deploy the billing service", ThreadID: "threadX"}
	mockStore := &MockStore{Tasks: []store.ConsolidatedMessage{}}
	mockAI := &MockAI{Sequence: []ai.TaskTransition{{Status: "RESOLVE"}}}
	finder := &stubOpenTaskFinder{candidates: []OpenTaskCandidate{{Task: openTask, Score: -0.1}}}
	svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)
	svc.SetEmbedder(finder)

	msg := store.ConsolidatedMessage{
		UserEmail:    "jjsong@whatap.io",
		ThreadID:     "threadY",
		Source:       "slack",
		OriginalText: "혹시 billing service 진행 상황 공유 가능할까요?", // question, no completion signal
	}

	svc.ProcessPotentialCompletion(ctx, msg)
	if _, ok := mockStore.Candidates[7]; ok {
		t.Error("no completion signal: must not record a candidate")
	}
}
