package services

import (
	"context"
	"errors"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

const (
	guardDoneTitle = "Complete MAP Group Asia Third Party Risk Management Questionnaire (UpGuard) by 27 Oct 2026"
	guardNewTitle  = "Complete the UpGuard security questionnaire requested by MAP Group Asia"
)

type guardSeed struct {
	source, requester, task, thread, room string
	done, deleted                         int
	completedAgo                          string // sqlite modifier, e.g. "-3 days"
}

func seedGuardTask(t *testing.T, email string, s guardSeed) store.MessageID {
	t.Helper()
	completed := "NULL"
	if s.completedAgo != "" {
		completed = fmt.Sprintf("datetime('now', '%s')", s.completedAgo)
	}
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, source, room, task, requester, thread_id, original_text, source_ts, done, is_deleted, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'original', ?, ?, ?, `+completed+`)`,
		email, s.source, s.room, s.task, s.requester, s.thread, testutil.RandomTS("guard"), s.done, s.deleted)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id, _ := res.LastInsertId()
	return store.MessageID(id)
}

func baseSeed() guardSeed {
	return guardSeed{source: "gmail", room: "Gmail", requester: "UpGuard CyberRisk", task: guardDoneTitle, thread: "thread-old", done: 1, completedAgo: "-3 days"}
}

func TestIsReminderOfDone_CandidateSelection(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(s *guardSeed)
		state      string
		msgThread  string
		newTitle   string
		wantCalled bool
	}{
		{"matching closed task reaches AI", func(s *guardSeed) {}, "new", "thread-new", guardNewTitle, true},
		{"requester mismatch", func(s *guardSeed) { s.requester = "Someone Else" }, "new", "thread-new", guardNewTitle, false},
		{"source mismatch", func(s *guardSeed) { s.source = "slack" }, "new", "thread-new", guardNewTitle, false},
		{"closed more than 31 days ago", func(s *guardSeed) { s.completedAgo = "-40 days" }, "new", "thread-new", guardNewTitle, false},
		{"deleted task", func(s *guardSeed) { s.deleted = 1 }, "new", "thread-new", guardNewTitle, false},
		{"task not done", func(s *guardSeed) { s.done = 0; s.completedAgo = "" }, "new", "thread-new", guardNewTitle, false},
		{"same thread handled elsewhere", func(s *guardSeed) {}, "new", "thread-old", guardNewTitle, false},
		{"closed task without room cannot take the append", func(s *guardSeed) { s.room = "" }, "new", "thread-new", guardNewTitle, false},
		{"overlap below floor", func(s *guardSeed) {}, "new", "thread-new", "Send pricing deck to Telkomsel", false},
		{"state update skips guard", func(s *guardSeed) {}, "update", "thread-new", guardNewTitle, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupCorrectionLearningTestDB(t)
			defer cleanup()
			email := testutil.RandomEmail("followup")
			seed := baseSeed()
			tc.mutate(&seed)
			seedGuardTask(t, email, seed)
			mock := &MockAI{Followup: ai.FollowupVerdict{Verdict: ai.FollowupNew}}
			svc := NewCompletionService(mock, &DefaultTaskStore{}, &TasksService{}, store.GetDB())
			item := store.TodoItem{State: tc.state, Task: tc.newTitle, Requester: "UpGuard CyberRisk"}
			msg := store.ConsolidatedMessage{UserEmail: email, Source: "gmail", Room: "Gmail", Requester: "UpGuard CyberRisk", ThreadID: tc.msgThread, OriginalText: "reminder"}

			isReminder := svc.IsReminderOfDone(context.Background(), item, msg)

			if called := mock.FollowupCallCount > 0; called != tc.wantCalled {
				t.Errorf("AI called = %v, want %v", called, tc.wantCalled)
			}
			if isReminder {
				t.Error("NEW verdict must not absorb the message")
			}
		})
	}
}

func TestIsReminderOfDone_OpenThreadTaskSkipsGuard(t *testing.T) {
	cleanup := setupCorrectionLearningTestDB(t)
	defer cleanup()
	email := testutil.RandomEmail("followup")
	seedGuardTask(t, email, baseSeed())
	open := baseSeed()
	open.done, open.completedAgo, open.thread, open.task = 0, "", "thread-new", "Open task in thread"
	seedGuardTask(t, email, open)
	mock := &MockAI{Followup: ai.FollowupVerdict{Verdict: ai.FollowupReminder}}
	svc := NewCompletionService(mock, &DefaultTaskStore{}, &TasksService{}, store.GetDB())
	msg := store.ConsolidatedMessage{UserEmail: email, Source: "gmail", Requester: "UpGuard CyberRisk", ThreadID: "thread-new", OriginalText: "reminder"}

	isReminder := svc.IsReminderOfDone(context.Background(), store.TodoItem{State: "new", Task: guardNewTitle}, msg)

	if isReminder || mock.FollowupCallCount != 0 {
		t.Errorf("isReminder=%v aiCalls=%d, want false/0 when the thread already has an open task", isReminder, mock.FollowupCallCount)
	}
}

func TestIsReminderOfDone_Verdicts(t *testing.T) {
	tests := []struct {
		name         string
		verdict      ai.FollowupVerdict
		err          error
		wantReminder bool
	}{
		{"REMINDER absorbs", ai.FollowupVerdict{Verdict: ai.FollowupReminder, Reason: "same questionnaire"}, nil, true},
		{"NEW creates", ai.FollowupVerdict{Verdict: ai.FollowupNew}, nil, false},
		{"AI error fails open", ai.FollowupVerdict{}, errors.New("boom"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupCorrectionLearningTestDB(t)
			defer cleanup()
			email := testutil.RandomEmail("followup")
			doneID := seedGuardTask(t, email, baseSeed())
			mock := &MockAI{Followup: tc.verdict, FollowupErr: tc.err}
			svc := NewCompletionService(mock, &DefaultTaskStore{}, &TasksService{}, store.GetDB())
			msg := store.ConsolidatedMessage{UserEmail: email, Source: "gmail", Room: "Gmail", Requester: "UpGuard CyberRisk", ThreadID: "thread-new", OriginalText: "friendly reminder text"}

			isReminder := svc.IsReminderOfDone(context.Background(), store.TodoItem{State: "new", Task: guardNewTitle}, msg)

			if isReminder != tc.wantReminder {
				t.Fatalf("isReminder = %v, want %v", isReminder, tc.wantReminder)
			}
			var original string
			var rows int
			if err := store.GetDB().QueryRow(`SELECT COALESCE(original_text,'') FROM messages WHERE id = ?`, int64(doneID)).Scan(&original); err != nil {
				t.Fatalf("read done task: %v", err)
			}
			if err := store.GetDB().QueryRow(`SELECT COUNT(*) FROM messages WHERE user_email = ?`, email).Scan(&rows); err != nil {
				t.Fatalf("count: %v", err)
			}
			if rows != 1 {
				t.Errorf("rows = %d, want 1 (guard never inserts)", rows)
			}
			appended := original != "original"
			if appended != tc.wantReminder {
				t.Errorf("original_text appended = %v, want %v (text=%q)", appended, tc.wantReminder, original)
			}
		})
	}
}
