package services

import (
	"context"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"
)

// RegressionMockAI simulates specialized AI responses for a realistic conversational scenario.
type RegressionMockAI struct {
	TurnCount int
	Results   map[int][]store.TodoItem
}

func (m *RegressionMockAI) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	m.TurnCount++
	res, ok := m.Results[m.TurnCount]
	if !ok {
		return []store.TodoItem{{State: "none"}}, nil
	}
	// Important: In a real scenario, AI would look at 'tasks' to find IDs.
	// We simulate this by returning the pre-defined results for each turn.
	return res, nil
}

func TestConversationalTaskLifecycle_Regression(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := "jj@example.com"
	room := "work_chat"
	source := "whatsapp"

	// Define a realistic 7-turn scenario
	// Task A (Report): Turn 2 (Create) -> Turn 5 (Resolve)
	// Task B (Meeting): Turn 4 (Create) -> Turn 7 (Resolve)
	mockAI := &RegressionMockAI{
		Results: map[int][]store.TodoItem{
			1: {{State: "none"}},                                       // Bob: "Hi"
			2: {{State: "new", Task: "보고서 공유"}},                        // Bob: "Report please"
			3: {{State: "none"}},                                       // JJ: "Checking..."
			4: {{State: "new", Task: "다음 주 미팅 일정 수립"}},                  // Bob: "Also schedule meeting"
			5: {{State: "resolve"}},                                  // JJ: "Here's report" (ID set later)
			6: {{State: "none"}},                                       // JJ: "How about Tue 2PM?"
			7: {{State: "resolve"}},                                  // Bob: "Sounds good!" (ID set later)
		},
	}
	svc := NewCompletionService(mockAI, &DefaultTaskStore{})

	scenario := []struct {
		Name     string
		Sender   string
		Text     string
		Expected string // "none", "new", or "resolve" (for verification)
	}{
		{"Turn1_Bob_Greet", "Bob", "안녕하세요 재진님!", "none"},
		{"Turn2_Bob_Req1", "Bob", "혹시 저번에 말씀하신 그 보고서 공유 가능할까요?", "new"},
		{"Turn3_JJ_Ack", "JJ", "네, 지금 확인 중입니다.", "none"},
		{"Turn4_Bob_Req2", "Bob", "아, 그리고 다음 주 미팅 일정도 잡아주시면 감사하겠습니다.", "new"},
		{"Turn5_JJ_Res1", "JJ", "넵! 보고서는 여기 있습니다: [Link]", "resolve"},
		{"Turn6_JJ_Prop", "JJ", "미팅은 화요일 오후 2시 어떠신가요?", "none"},
		{"Turn7_Bob_Res2", "Bob", "네, 좋습니다! 확인했습니다.", "resolve"},
	}

	var taskAID, taskBID int

	for i, turn := range scenario {
		turnNum := i + 1
		t.Run(turn.Name, func(t *testing.T) {
			// 1. Save Message
			_, msgID, _ := store.SaveMessage(store.ConsolidatedMessage{
				UserEmail:    email,
				Source:       source,
				Room:         room,
				Requester:    turn.Sender,
				OriginalText: turn.Text,
				SourceTS:     fmt.Sprintf("ts_%d", turnNum),
				ThreadID:     "thread_realistic",
			})
			msg, _ := store.GetMessageByID(ctx, msgID)

			// 2. Mock state preparation (Inject IDs for resolution turns)
			if turnNum == 5 { // JJ resolves Task A
				res := mockAI.Results[5][0]
				res.ID = &taskAID
				mockAI.Results[5][0] = res
			}
			if turnNum == 7 { // Bob resolves Task B
				res := mockAI.Results[7][0]
				res.ID = &taskBID
				mockAI.Results[7][0] = res
			}

			// 3. Process
			svc.ProcessPotentialCompletion(ctx, msg)

			// 4. Verification & ID Recording
			if turn.Expected == "new" {
				updated, _ := store.GetMessageByID(ctx, msgID)
				if updated.Task == "" {
					t.Errorf("%s: Task should have been created", turn.Name)
				}
				if turnNum == 2 { taskAID = msgID }
				if turnNum == 4 { taskBID = msgID }
			}

			if turn.Expected == "none" {
				updated, _ := store.GetMessageByID(ctx, msgID)
				if updated.Task != "" {
					t.Errorf("%s: Greet/Ack should not create tasks", turn.Name)
				}
			}

			if turn.Expected == "resolve" {
				var targetID int
				if turnNum == 5 { targetID = taskAID }
				if turnNum == 7 { targetID = taskBID }
				
				m, _ := store.GetMessageByID(ctx, targetID)
				if !m.Done {
					t.Errorf("%s: Task %d should be resolved", turn.Name, targetID)
				}
			}
		})
	}
}
