package services

import (
	"context"
	"errors"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func TestReviewFoldedAssignee(t *testing.T) {
	manualMeta, err := MetadataSet(nil, "field_sources", map[string]string{"assignee": "manual"})
	if err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	alice := store.ConsolidatedMessage{ID: 7, Task: "Send report", Assignee: "alice"}
	const handover = "bob will handle this"
	tests := []struct {
		name        string
		parent      store.ConsolidatedMessage
		text        string
		requester   string
		extracted   string
		aiResult    ai.TaskTransition
		aiErr       error
		wantAICalls int
		wantApplied bool
		wantErr     bool
	}{
		{"different assignee asks AI and applies", alice, handover, "", "bob", ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, nil, 1, true, false},
		{"same assignee skips AI", store.ConsolidatedMessage{ID: 7, Task: "Send report", Assignee: "bob"}, handover, "", "bob", ai.TaskTransition{Status: "UPDATE", AssignedTo: "carol"}, nil, 0, false, false},
		{"empty extracted assignee skips AI", alice, handover, "", "", ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, nil, 0, false, false},
		{"manual parent assignee skips AI", store.ConsolidatedMessage{ID: 7, Task: "Send report", Assignee: "alice", Metadata: manualMeta}, handover, "", "bob", ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, nil, 0, false, false},
		{"AI without assigned_to leaves assignee", alice, handover, "", "bob", ai.TaskTransition{Status: "UPDATE"}, nil, 1, false, false},
		{"RESOLVE with assigned_to is not applied", alice, handover, "", "bob", ai.TaskTransition{Status: "RESOLVE", AssignedTo: "bob"}, nil, 1, false, false},
		{"NONE with assigned_to is not applied", alice, handover, "", "bob", ai.TaskTransition{Status: "NONE", AssignedTo: "bob"}, nil, 1, false, false},
		{"unmentioned non-sender skips AI", alice, "please check the numbers", "carol", "bob", ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, nil, 0, false, false},
		{"sender equal to extracted asks AI without mention", alice, "I'll take this over", "bob", "bob", ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, nil, 1, true, false},
		{"AI error is returned and nothing applied", alice, handover, "", "bob", ai.TaskTransition{}, errors.New("boom"), 1, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockAI := &MockAI{Sequence: []ai.TaskTransition{tc.aiResult}, Err: tc.aiErr}
			mockStore := &MockStore{Tasks: []store.ConsolidatedMessage{tc.parent}}
			svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)
			msg := store.ConsolidatedMessage{UserEmail: testutil.RandomEmail("fold-review"), ThreadID: "t1", OriginalText: tc.text, Requester: tc.requester}

			applied, err := svc.ReviewFoldedAssignee(context.Background(), msg, tc.parent.ID, tc.extracted)

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if mockAI.CallCount != tc.wantAICalls {
				t.Errorf("AI calls = %d, want %d", mockAI.CallCount, tc.wantAICalls)
			}
			if applied != tc.wantApplied {
				t.Errorf("applied = %v, want %v", applied, tc.wantApplied)
			}
			if !tc.wantApplied {
				if len(mockStore.UpdateItems) != 0 {
					t.Errorf("unexpected update items: %+v", mockStore.UpdateItems)
				}
				return
			}
			got := mockStore.UpdateItems[0]
			if got.AssignedTo != "bob" || got.Task != "" {
				t.Errorf("update item = %+v, want AssignedTo bob and empty Task (append-only)", got)
			}
			if mockAI.LastParentAssignee != tc.parent.Assignee {
				t.Errorf("AI parentAssignee = %q, want %q", mockAI.LastParentAssignee, tc.parent.Assignee)
			}
		})
	}
}

func TestReviewFoldedAssignee_ParentGoneIsNoop(t *testing.T) {
	mockAI := &MockAI{}
	svc := NewCompletionService(mockAI, &MockStore{}, &TasksService{}, nil)

	applied, err := svc.ReviewFoldedAssignee(context.Background(), store.ConsolidatedMessage{UserEmail: "a@b.c", ThreadID: "t"}, 9, "bob")

	if err != nil || applied || mockAI.CallCount != 0 {
		t.Errorf("applied=%v err=%v aiCalls=%d, want no-op", applied, err, mockAI.CallCount)
	}
}

// Why: real-store test, because the manual-assignee guard lives in the routing layer
// that MockStore bypasses.
func TestHandleCompletionResult_AssignedToReassigns(t *testing.T) {
	manualMeta, err := MetadataSet(nil, "field_sources", map[string]string{"assignee": "manual"})
	if err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	tests := []struct {
		name         string
		meta         []byte
		res          ai.TaskTransition
		wantHandled  bool
		wantAssignee string
		wantTask     string
	}{
		{"assigned_to reassigns with new title", nil, ai.TaskTransition{Status: "UPDATE", UpdatedText: "Renamed", AssignedTo: "bob"}, true, "bob", "Renamed"},
		{"assigned_to only keeps title", nil, ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, true, "bob", "Original task"},
		{"manual assignee blocks reassignment", manualMeta, ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, true, "alice", "Original task"},
		{"both empty is no-op", nil, ai.TaskTransition{Status: "UPDATE"}, false, "alice", "Original task"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupCorrectionLearningTestDB(t)
			defer cleanup()
			email := testutil.RandomEmail("reassign")
			r, err := store.GetDB().Exec(
				`INSERT INTO messages (user_email, source, room, task, assignee, original_text, source_ts, done, is_deleted, metadata)
				 VALUES (?, 'whatsapp', 'general', 'Original task', 'alice', 'orig', ?, 0, 0, ?)`,
				email, testutil.RandomTS("reassign"), string(tc.meta))
			if err != nil {
				t.Fatalf("seed: %v", err)
			}
			id64, _ := r.LastInsertId()
			parent := store.ConsolidatedMessage{ID: store.MessageID(id64), Task: "Original task", Assignee: "alice"}
			msg := store.ConsolidatedMessage{UserEmail: email, Source: "whatsapp", Room: "general", OriginalText: "bob will handle this"}
			svc := NewCompletionService(&MockAI{}, &DefaultTaskStore{}, &TasksService{}, store.GetDB())

			handled := svc.handleCompletionResult(context.Background(), tc.res, msg, parent)

			var assignee, task string
			if err := store.GetDB().QueryRow("SELECT COALESCE(assignee,''), task FROM messages WHERE id = ?", id64).Scan(&assignee, &task); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if handled != tc.wantHandled || assignee != tc.wantAssignee || task != tc.wantTask {
				t.Errorf("handled=%v assignee=%q task=%q, want %v %q %q", handled, assignee, task, tc.wantHandled, tc.wantAssignee, tc.wantTask)
			}
		})
	}
}

func TestApplyThreadReplyOutcome_AppliesAssignedTo(t *testing.T) {
	mockStore := &MockStore{}
	svc := NewCompletionService(&MockAI{}, mockStore, &TasksService{}, nil)
	task := store.ConsolidatedMessage{ID: 11, Task: "Send report", Assignee: "alice"}
	msg := store.ConsolidatedMessage{UserEmail: "a@b.c", Requester: "carol"}

	handled := svc.applyThreadReplyOutcome(context.Background(), ai.TaskTransition{Status: "UPDATE", AssignedTo: "bob"}, msg, task)

	if !handled || len(mockStore.UpdateItems) != 1 {
		t.Fatalf("handled=%v updates=%d, want handled with one update", handled, len(mockStore.UpdateItems))
	}
	if got := mockStore.UpdateItems[0]; got.AssignedTo != "bob" || got.Task != "" {
		t.Errorf("update item = %+v, want AssignedTo bob and empty Task", got)
	}
}
