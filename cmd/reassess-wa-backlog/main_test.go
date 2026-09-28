package main

import (
	"context"
	"testing"
	"time"

	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

func initTestDB(t *testing.T) {
	t.Helper()
	store.ResetForTest()
	if err := store.InitDB(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
}

// insertOpenWATask inserts a minimal open WhatsApp task row, so openWATasks can
// exercise the source/done/room filters end to end.
func insertOpenWATask(ctx context.Context, t *testing.T, email, room, sourceTS, assignee string, assignedAt time.Time) store.MessageID {
	t.Helper()
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, source_ts, assignee, assigned_at, done, is_deleted, created_at)
		 VALUES (?, 'whatsapp', ?, 'still open task', ?, ?, ?, 0, 0, datetime('now'))`,
		email, room, sourceTS, assignee, assignedAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("insertOpenWATask: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return store.MessageID(id)
}

// fakeAI is a minimal services.AICompleter fake so tool tests never call a real
// provider; it returns a fixed transition for every EvaluateTaskTransition call.
type fakeAI struct {
	transition ai.TaskTransition
	callCount  int
}

func (f *fakeAI) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	return nil, nil
}

func (f *fakeAI) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (ai.TaskTransition, error) {
	f.callCount++
	return f.transition, nil
}

func (f *fakeAI) Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error) {
	return nil, nil
}

func TestOpenWATasks_FiltersSourceDoneAndStatusChat(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	insertOpenWATask(ctx, t, "u@x", "Family Group", "wa1", "", time.Now())
	// done task -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, done, is_deleted, created_at) VALUES ('u@x', 'whatsapp', 'r', 't', 1, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed done task: %v", err)
	}
	// slack source -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, done, is_deleted, created_at) VALUES ('u@x', 'slack', 'r', 't', 0, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed slack task: %v", err)
	}
	// status pseudo-chat -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, done, is_deleted, created_at) VALUES ('u@x', 'whatsapp', 'status', 't', 0, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed status task: %v", err)
	}

	tasks, err := openWATasks(ctx, "u@x", 97)
	if err != nil {
		t.Fatalf("openWATasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 open WhatsApp task, got %d", len(tasks))
	}
	if tasks[0].Room != "Family Group" {
		t.Errorf("Room = %q, want %q", tasks[0].Room, "Family Group")
	}
}

// TestSelectCandidateReplies_PriorityOrderAndCap verifies the four priority tiers are
// applied in order and the result is capped at replyCandidateCap.
func TestSelectCandidateReplies_PriorityOrderAndCap(t *testing.T) {
	task := store.ConsolidatedMessage{
		Task: "renew the office lease agreement", Assignee: "Budi", Requester: "Siti",
		ThreadID: "src1", SourceTS: "src1",
	}
	replies := []store.WAChatMessage{
		{MessageID: "r1", Sender: "Random", Body: "just chatting, unrelated", TS: 1},
		{MessageID: "r2", Sender: "Budi", Body: "will do", TS: 2},                                    // tier 2: assignee
		{MessageID: "r3", Sender: "Random", Body: "already done, all set", TS: 3},                    // tier 3: completion signal
		{MessageID: "r4", Sender: "Random", Body: "about the lease agreement", TS: 4},                // tier 4: topical overlap
		{MessageID: "r5", Sender: "Random", Body: `{"a":1}`, TS: 5, RawJSON: `{"ReplyToID":"src1"}`}, // tier 1: reply anchor
	}

	got := selectCandidateReplies(task, replies)
	if len(got) != 4 {
		t.Fatalf("expected 4 selected candidates (r1 has no signal), got %d: %+v", len(got), got)
	}
	if got[0].MessageID != "r5" {
		t.Errorf("tier 1 (reply anchor) must be first, got %q", got[0].MessageID)
	}
	if got[1].MessageID != "r2" {
		t.Errorf("tier 2 (assignee) must be second, got %q", got[1].MessageID)
	}
	if got[2].MessageID != "r3" {
		t.Errorf("tier 3 (completion signal) must be third, got %q", got[2].MessageID)
	}
	if got[3].MessageID != "r4" {
		t.Errorf("tier 4 (topical overlap) must be last, got %q", got[3].MessageID)
	}
}

// TestSelectCandidateReplies_ExcludesOwnSourceAndEmptyBody verifies the task's own
// source message and empty-body rows never become candidates.
func TestSelectCandidateReplies_ExcludesOwnSourceAndEmptyBody(t *testing.T) {
	task := store.ConsolidatedMessage{Task: "ship the report", Assignee: "Budi", SourceTS: "src1"}
	replies := []store.WAChatMessage{
		{MessageID: "src1", Sender: "Budi", Body: "will do"}, // own source message -> excluded
		{MessageID: "r2", Sender: "Budi", Body: ""},          // empty body -> excluded
	}

	got := selectCandidateReplies(task, replies)
	if len(got) != 0 {
		t.Fatalf("expected 0 candidates, got %d: %+v", len(got), got)
	}
}

// TestSelectCandidateReplies_CapAtReplyCandidateCap verifies the cap applies even when
// every reply matches the same tier.
func TestSelectCandidateReplies_CapAtReplyCandidateCap(t *testing.T) {
	task := store.ConsolidatedMessage{Task: "renew the lease", Assignee: "Budi"}
	var replies []store.WAChatMessage
	for i := 0; i < replyCandidateCap+5; i++ {
		replies = append(replies, store.WAChatMessage{MessageID: "r", Sender: "Budi", Body: "will do"})
	}

	got := selectCandidateReplies(task, replies)
	if len(got) != replyCandidateCap {
		t.Fatalf("expected exactly %d candidates, got %d", replyCandidateCap, len(got))
	}
}

// TestEvaluateReplyAgainstTask_dryRunRecordsButNeverWrites verifies the tool calls the
// SAME evaluator as the sweep (EvaluateThreadReply) and a dry run records the row but
// writes nothing.
func TestEvaluateReplyAgainstTask_dryRunRecordsButNeverWrites(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenWATask(ctx, t, "u@x", "room", "src1", "Someone Else", time.Now().Add(-time.Hour))

	fa := &fakeAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: false}
	svc := services.NewCompletionService(fa, bs, &services.TasksService{}, store.GetDB())

	task := store.ConsolidatedMessage{ID: id, Task: "still open task", Assignee: "Someone Else"}
	reply := store.WAChatMessage{MessageID: "r1", Sender: "Counterparty", Body: "already resolved, no keyword here", TS: time.Now().Unix()}

	evaluateReplyAgainstTask(ctx, svc, bs, task, reply)

	if fa.callCount == 0 {
		t.Fatal("expected EvaluateTaskTransition to be called")
	}
	if len(bs.rows) != 1 {
		t.Fatalf("expected 1 recorded row, got %d", len(bs.rows))
	}
	if bs.written != 0 {
		t.Errorf("dry run must not write, got written=%d", bs.written)
	}
	if !bs.isResolved(id) {
		t.Error("expected the RESOLVE verdict to mark the task resolved for the stop-after-first-RESOLVE loop")
	}
	var done int
	row := store.GetDB().QueryRowContext(ctx, `SELECT done FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&done); err != nil {
		t.Fatalf("scan done: %v", err)
	}
	if done != 0 {
		t.Error("dry run must never modify the task")
	}
}

// TestBacklogStore_ApplyDowngradesAssigneeResolveToCandidate verifies -apply never
// hard-closes even a RESOLVE from the task's own assignee: it writes a confirm-first
// candidate instead and leaves done=0.
func TestBacklogStore_ApplyDowngradesAssigneeResolveToCandidate(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenWATask(ctx, t, "u@x", "room", "src1", "Me", time.Now().Add(-time.Hour))

	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: true}
	bs.currentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me"}
	bs.currentSender = "Me"

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "r1"}

	if _, err := bs.HandleTaskState(ctx, store.GetDB(), "u@x", item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}

	if bs.written != 1 {
		t.Errorf("expected 1 candidate written under -apply, got %d", bs.written)
	}
	var done int
	var metadata string
	row := store.GetDB().QueryRowContext(ctx, `SELECT done, COALESCE(metadata, '') FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&done, &metadata); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if done != 0 {
		t.Error("expected done=0: the tool must never hard-close, even for the assignee's own RESOLVE")
	}
	if metadata == "" {
		t.Error("expected a confirm-first candidate recorded in metadata")
	}
}

// TestBacklogStore_ApplyRespectsDismissal verifies a previously dismissed source is not
// re-recorded even under -apply.
func TestBacklogStore_ApplyRespectsDismissal(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenWATask(ctx, t, "u@x", "room", "src1", "Me", time.Now().Add(-time.Hour))
	if _, err := store.GetDB().ExecContext(ctx,
		`UPDATE messages SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.completion_dismissed_source', 'r1') WHERE id = ?`,
		int64(id)); err != nil {
		t.Fatalf("seed dismissed marker: %v", err)
	}

	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: true}
	bs.currentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me", Metadata: []byte(`{"completion_dismissed_source":"r1"}`)}

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "r1"}

	if _, err := bs.HandleTaskState(ctx, store.GetDB(), "u@x", item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if bs.written != 0 {
		t.Errorf("expected the dismissed source to stay suppressed, got written=%d", bs.written)
	}
}

func TestPrintResults_EmptyIsHandled(t *testing.T) {
	bs := &backlogStore{}
	bs.printResults() // must not panic on an empty result set
}
