package scanner

import (
	"context"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

func TestSaveChannelItem_ReminderOfDoneGuard(t *testing.T) {
	const email = "reminder-guard@example.com"
	const doneTitle = "Complete MAP Group Asia Third Party Risk Management Questionnaire (UpGuard) by 27 Oct 2026"
	tests := []struct {
		name         string
		verdict      string
		replay       bool
		wantAICalls  int
		wantNewTasks int
	}{
		{"reminder verdict short-circuits creation", ai.FollowupReminder, false, 1, 0},
		{"new verdict still creates the task", ai.FollowupNew, false, 1, 1},
		{"replayed message skips the guard", ai.FollowupReminder, true, 0, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initTestDB(t)
			ctx := context.Background()
			user, err := store.GetOrCreateUser(ctx, email, "Guard User", "")
			if err != nil {
				t.Fatalf("GetOrCreateUser: %v", err)
			}
			res, err := store.GetDB().ExecContext(ctx,
				`INSERT INTO messages (user_email, source, room, task, requester, source_ts, original_text, thread_id, done, is_deleted, completed_at)
				 VALUES (?, ?, 'room', ?, 'UpGuard CyberRisk', 'ts-done', 'orig', 'thread-old', 1, 0, datetime('now', '-3 days'))`,
				email, store.SourceWhatsApp, doneTitle)
			if err != nil {
				t.Fatalf("seed done task: %v", err)
			}
			doneID, _ := res.LastInsertId()

			fake := &fakeThreadReplyAI{followup: ai.FollowupVerdict{Verdict: tc.verdict, Reason: "test"}}
			origSvc := deps.completionSvc
			t.Cleanup(func() { deps.completionSvc = origSvc })
			deps.completionSvc = services.NewCompletionService(fake, &services.DefaultTaskStore{}, nil, store.GetDB())

			item := store.TodoItem{State: "new", Task: "Complete the UpGuard security questionnaire requested by MAP Group Asia", Requester: "UpGuard CyberRisk", Assignee: "Guard User", SourceTS: "ts-new"}
			raw := types.RawMessage{ID: "ts-new", Sender: "UpGuard CyberRisk", Text: "Friendly reminder to complete the questionnaire", IsReplay: tc.replay, ThreadID: "thread-new"}

			got := saveChannelItem(ctx, *user, nil, item, raw, "room", false, whatsAppAdapter{})

			if fake.followupCalls != tc.wantAICalls {
				t.Errorf("followup AI calls = %d, want %d", fake.followupCalls, tc.wantAICalls)
			}
			var newTasks int
			if err := store.GetDB().QueryRowContext(ctx,
				`SELECT COUNT(*) FROM messages WHERE user_email = ? AND id != ?`, email, doneID).Scan(&newTasks); err != nil {
				t.Fatalf("count: %v", err)
			}
			if (got > 0) != (tc.wantNewTasks > 0) {
				t.Errorf("returned id = %d, want >0 only when a task is created", got)
			}
			if newTasks != tc.wantNewTasks {
				t.Errorf("new tasks = %d, want %d", newTasks, tc.wantNewTasks)
			}
		})
	}
}
