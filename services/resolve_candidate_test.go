package services

import (
	"context"
	"strings"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

// Why: Indofood-PO regression — a single UPDATE verdict computed against one FTS
// candidate was fanned out to every candidate, appending an unrelated conversation
// to unrelated tasks. The verdict must land on the evaluated task only.
func TestCrossChannelUpdateDoesNotFanOut(t *testing.T) {
	ctx := context.Background()

	related := store.ConsolidatedMessage{ID: 1, Task: "Deploy the billing service", ThreadID: "threadA"}
	unrelated := store.ConsolidatedMessage{ID: 2, Task: "Implement APM in SAMCO environment", ThreadID: "threadB"}
	mockStore := &MockStore{OpenFTSResults: []store.ConsolidatedMessage{related, unrelated}}
	mockAI := &MockAI{Sequence: []ai.TaskTransition{{Status: "UPDATE", UpdatedText: "Deploy the billing service to prod"}}}
	svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

	msg := store.ConsolidatedMessage{
		UserEmail:    "jjsong@whatap.io",
		Source:       "whatsapp",
		OriginalText: "billing service 배포 완료했습니다",
	}

	handled, err := svc.ProcessCrossChannelSignal(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true for the topically related candidate")
	}
	if len(mockStore.ReleasedIDs) != 1 || mockStore.ReleasedIDs[0] != 1 {
		t.Errorf("UPDATE must apply only to the evaluated task, updated IDs = %v", mockStore.ReleasedIDs)
	}
}

// Why: when no FTS candidate shares topical tokens with the message, the LLM must not
// be consulted at all — a generic completion phrase over BM25 noise is how the
// Indofood-PO text reached two unrelated tasks.
func TestCrossChannelSkipsWhenNoTopicalCandidate(t *testing.T) {
	ctx := context.Background()

	offTopic1 := store.ConsolidatedMessage{ID: 12678, Task: "Clarify UAT environment differences", ThreadID: "threadA"}
	offTopic2 := store.ConsolidatedMessage{ID: 12719, Task: "Implement APM in SAMCO environment", ThreadID: "threadB"}
	mockStore := &MockStore{OpenFTSResults: []store.ConsolidatedMessage{offTopic1, offTopic2}}
	mockAI := &MockAI{Sequence: []ai.TaskTransition{{Status: "UPDATE", UpdatedText: "should never be used"}}}
	svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

	// Real Indofood-PO text (completion signal "sent" added to pass the keyword gate,
	// mirroring how generic wording slipped through in production).
	msg := store.ConsolidatedMessage{
		UserEmail:    "jjsong@whatap.io",
		Source:       "whatsapp",
		OriginalText: "PO sent. bisa tolong share PO yang dari Indofood ke Skyworx kah? Soalnya kayaknya ada miscomm.",
	}

	handled, err := svc.ProcessCrossChannelSignal(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false when every candidate is off-topic")
	}
	if mockAI.CallCount != 0 {
		t.Errorf("LLM must not be consulted without a topical candidate, called %d times", mockAI.CallCount)
	}
	if len(mockStore.ReleasedIDs) != 0 || len(mockStore.CapturedIDs) != 0 || len(mockStore.Candidates) != 0 {
		t.Errorf("no task may be touched: updated=%v resolved=%v candidates=%v",
			mockStore.ReleasedIDs, mockStore.CapturedIDs, mockStore.Candidates)
	}
}

// Why: WhatsApp quote-reply completion — a task anchored on its source message ID
// (SaveThreadID) must be found and resolved when the user's reply carries that ID as
// ThreadID (dispatchThreadedCompletion sets ThreadID=ReplyToID).
func TestThreadedReplyCompletesWhatsAppTask(t *testing.T) {
	ctx := context.Background()

	task := store.ConsolidatedMessage{ID: 88, Task: "Send updated license file", ThreadID: "3EB0WAORIGIN"}
	mockStore := &MockStore{Tasks: []store.ConsolidatedMessage{task}}
	mockAI := &MockAI{Sequence: []ai.TaskTransition{{Status: "RESOLVE"}}}
	svc := NewCompletionService(mockAI, mockStore, &TasksService{}, nil)

	msg := store.ConsolidatedMessage{
		UserEmail:          "jjsong@whatap.io",
		RequesterCanonical: "jjsong@whatap.io",
		Source:             "whatsapp",
		ThreadID:           "3EB0WAORIGIN",
		OriginalText:       "라이선스 파일 보내드렸습니다, 확인 부탁드려요",
	}

	handled, err := svc.ProcessPotentialCompletion(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatal("expected handled=true for threaded WhatsApp completion")
	}
	if len(mockStore.CapturedIDs) != 1 || mockStore.CapturedIDs[0] != 88 {
		t.Errorf("resolved IDs = %v, want [88]", mockStore.CapturedIDs)
	}
}

// Why: task 12761 end-to-end — a demoted resolve (resolve_candidate) must leave the
// task open and record a pending metadata.completion_candidate the UI can confirm.
func TestHandleResolveCandidate_RecordsPendingKeepsOpen(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "resolve-candidate@example.com"
	room := "[Technical] Skyworx x WhaTap"
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, thread_id, done, is_deleted) VALUES (?, 'whatsapp', ?, 'Schedule SAMCO discussion this week', 'orig', 'ts-cand', 'THREAD-Z', 0, 0)",
		email, room,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)

	idVal := id
	item := store.TodoItem{ID: &idVal, State: "resolve_candidate", Task: "Schedule SAMCO discussion this week"}
	msg := store.ConsolidatedMessage{
		UserEmail:    email,
		Source:       "whatsapp",
		Room:         room,
		SourceTS:     "3C967B9AD247FD9C4054",
		OriginalText: "Okkay jam 10",
	}

	got, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if got != id {
		t.Fatalf("HandleTaskState id = %d, want %d", got, id)
	}

	var done int
	var metadata string
	if err := store.GetDB().QueryRow("SELECT done, COALESCE(metadata, '') FROM messages WHERE id = ?", id).Scan(&done, &metadata); err != nil {
		t.Fatalf("read: %v", err)
	}
	if done != 0 {
		t.Errorf("done = %d, want 0 (confirm-first must not close)", done)
	}
	if !strings.Contains(metadata, "completion_candidate") || !strings.Contains(metadata, "pending") {
		t.Errorf("metadata = %q, want a pending completion_candidate", metadata)
	}
	if !strings.Contains(metadata, "3C967B9AD247FD9C4054") {
		t.Errorf("metadata = %q, want source_ts as dismissal key", metadata)
	}
}

// Why: a dismissed suggestion must not resurface from the same source message.
func TestHandleResolveCandidate_SuppressesDismissedSource(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "resolve-candidate-dismiss@example.com"
	room := "Dismiss-Room"
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'whatsapp', ?, 'T', 'o', 'ts-d', 0, 0)",
		email, room,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	id := store.MessageID(id64)
	ctx := context.Background()

	idVal := id
	item := store.TodoItem{ID: &idVal, State: "resolve_candidate", Task: "T"}
	msg := store.ConsolidatedMessage{
		UserEmail: email, Source: "whatsapp", Room: room,
		SourceTS: "SRC-1", OriginalText: "done and settled",
	}
	if _, err := HandleTaskState(ctx, nil, email, item, msg); err != nil {
		t.Fatalf("first candidate: %v", err)
	}
	if err := store.DismissCompletionCandidate(ctx, store.GetDB(), email, id); err != nil {
		t.Fatalf("dismiss: %v", err)
	}

	got, err := HandleTaskState(ctx, nil, email, item, msg)
	if err != nil {
		t.Fatalf("re-fire: %v", err)
	}
	if got != 0 {
		t.Errorf("re-fired dismissed candidate returned id %d, want 0 (suppressed)", got)
	}

	var metadata string
	if err := store.GetDB().QueryRow("SELECT COALESCE(metadata, '') FROM messages WHERE id = ?", id).Scan(&metadata); err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(metadata, "\"status\":\"pending\"") {
		t.Errorf("metadata = %q, dismissed candidate must not be re-recorded", metadata)
	}
}
