package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"message-consolidator/ai"
	"message-consolidator/cmd/internal/backlog"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"

	"google.golang.org/api/gmail/v1"
)

func initTestDB(t *testing.T) {
	t.Helper()
	store.ResetForTest()
	if err := store.InitDB(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
}

// insertOpenGmailTask inserts a minimal open Gmail task row with a thread ID, so
// openGmailTasks/groupByGmailThread can exercise it end to end.
func insertOpenGmailTask(ctx context.Context, t *testing.T, email, threadID, sourceTS, assignee string, assignedAt time.Time) store.MessageID {
	t.Helper()
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, source_ts, assignee, assigned_at, done, is_deleted, created_at)
		 VALUES (?, 'gmail', 'Gmail', 'still open task', ?, ?, ?, ?, 0, 0, datetime('now'))`,
		email, threadID, sourceTS, assignee, assignedAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("insertOpenGmailTask: %v", err)
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

// gmailMsg builds a minimal *gmail.Message with a plain-text body and a From header,
// for candidate-selection and evaluation tests.
func gmailMsg(id string, internalDate int64, from, body string) *gmail.Message {
	return &gmail.Message{
		Id:           id,
		InternalDate: internalDate,
		Payload: &gmail.MessagePart{
			MimeType: "text/plain",
			Headers:  []*gmail.MessagePartHeader{{Name: "From", Value: from}},
			Body:     &gmail.MessagePartBody{Data: b64(body)},
		},
	}
}

func b64(s string) string {
	// Why: mirrors gmail.v1's base64url (no padding) wire encoding for MessagePartBody.Data.
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out []byte
	data := []byte(s)
	for i := 0; i < len(data); i += 3 {
		var b [3]byte
		n := copy(b[:], data[i:])
		out = append(out, chars[b[0]>>2])
		out = append(out, chars[(b[0]&0x03)<<4|(b[1]>>4)])
		if n > 1 {
			out = append(out, chars[(b[1]&0x0f)<<2|(b[2]>>6)])
		}
		if n > 2 {
			out = append(out, chars[b[2]&0x3f])
		}
	}
	return string(out)
}

func TestOpenGmailTasks_FiltersSourceAndDoneAndRequiresThread(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	insertOpenGmailTask(ctx, t, "u@x", "t1", "gmail-m1", "", time.Now())
	// done task -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at) VALUES ('u@x', 'gmail', 'Gmail', 't', 't2', 1, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed done task: %v", err)
	}
	// slack source -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at) VALUES ('u@x', 'slack', 'r', 't', 't3', 0, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed slack task: %v", err)
	}
	// no thread_id -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at) VALUES ('u@x', 'gmail', 'Gmail', 't', '', 0, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed no-thread task: %v", err)
	}

	tasks, err := openGmailTasks(ctx, "u@x", 97)
	if err != nil {
		t.Fatalf("openGmailTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 open Gmail task with a thread, got %d", len(tasks))
	}
	if tasks[0].ThreadID != "t1" {
		t.Errorf("ThreadID = %q, want %q", tasks[0].ThreadID, "t1")
	}
}

func TestGroupByGmailThread(t *testing.T) {
	tasks := []store.ConsolidatedMessage{
		{ID: 1, ThreadID: "t1"},
		{ID: 2, ThreadID: "t1"},
		{ID: 3, ThreadID: "t2"},
	}
	groups := groupByGmailThread(tasks)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	total := 0
	for _, g := range groups {
		total += len(g.tasks)
	}
	if total != 3 {
		t.Errorf("expected 3 grouped tasks, got %d", total)
	}
}

// TestGmailReplyCandidates_AfterSourceOrderingAndExclusion verifies only messages
// strictly after the source message (by InternalDate) are selected, in chronological
// order, and the source message itself is excluded.
func TestGmailReplyCandidates_AfterSourceOrderingAndExclusion(t *testing.T) {
	messages := []*gmail.Message{
		gmailMsg("before", 100, "a@x.com", "earlier message"),
		gmailMsg("src", 200, "a@x.com", "the source message"),
		gmailMsg("r2", 400, "b@x.com", "second reply"),
		gmailMsg("r1", 300, "b@x.com", "first reply"),
	}

	got := gmailReplyCandidates("src", messages)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d: %+v", len(got), got)
	}
	if got[0].Id != "r1" || got[1].Id != "r2" {
		t.Errorf("expected chronological order [r1, r2], got [%s, %s]", got[0].Id, got[1].Id)
	}
}

// TestGmailReplyCandidates_SourceNotFound verifies a missing source message yields no
// candidates rather than treating every message as a candidate.
func TestGmailReplyCandidates_SourceNotFound(t *testing.T) {
	messages := []*gmail.Message{gmailMsg("r1", 100, "a@x.com", "body")}
	got := gmailReplyCandidates("missing", messages)
	if got != nil {
		t.Fatalf("expected nil candidates when source message is not in the thread, got %+v", got)
	}
}

// TestGmailReplyCandidates_CapPreservesNewestLastOrder verifies more than the cap
// qualifying replies are trimmed down to the most recent replyCandidateCap, still in
// newest-last (chronological) order.
func TestGmailReplyCandidates_CapPreservesNewestLastOrder(t *testing.T) {
	messages := []*gmail.Message{gmailMsg("src", 0, "a@x.com", "source")}
	total := replyCandidateCap + 5
	for i := 1; i <= total; i++ {
		messages = append(messages, gmailMsg(idFor(i), int64(i), "b@x.com", "reply"))
	}

	got := gmailReplyCandidates("src", messages)
	if len(got) != replyCandidateCap {
		t.Fatalf("expected exactly %d candidates, got %d", replyCandidateCap, len(got))
	}
	// The most recent replyCandidateCap messages are ids (total-cap+1)..total, ascending.
	wantFirst := idFor(total - replyCandidateCap + 1)
	wantLast := idFor(total)
	if got[0].Id != wantFirst || got[len(got)-1].Id != wantLast {
		t.Errorf("expected range [%s, %s], got [%s, %s]", wantFirst, wantLast, got[0].Id, got[len(got)-1].Id)
	}
}

func idFor(i int) string {
	return fmt.Sprintf("r%d", i)
}

// TestEvaluateGmailReplyAgainstTask_dryRunRecordsButNeverWrites verifies the tool
// calls the SAME evaluator as the sweep (EvaluateThreadReply), extracting the reply
// body via channels.ExtractCleanBody, and a dry run records the row but writes
// nothing.
func TestEvaluateGmailReplyAgainstTask_dryRunRecordsButNeverWrites(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenGmailTask(ctx, t, "u@x", "t1", "gmail-src", "Someone Else", time.Now().Add(-time.Hour))

	fa := &fakeAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	bs := backlog.NewStore(&services.DefaultTaskStore{}, store.GetDB(), false, "gmail")
	svc := services.NewCompletionService(fa, bs, &services.TasksService{}, store.GetDB())

	task := store.ConsolidatedMessage{ID: id, Task: "still open task", Assignee: "Someone Else", Room: "Gmail", ThreadID: "t1"}
	reply := gmailMsg("r1", 1000, "Counterparty <cp@x.com>", "already resolved, no keyword here")

	evaluateGmailReplyAgainstTask(ctx, svc, bs, task, reply)

	if fa.callCount == 0 {
		t.Fatal("expected EvaluateTaskTransition to be called")
	}
	rows := bs.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 recorded row, got %d", len(rows))
	}
	if rows[0].Speaker != "Counterparty" {
		t.Errorf("speaker = %q, want %q", rows[0].Speaker, "Counterparty")
	}
	if bs.Written() != 0 {
		t.Errorf("dry run must not write, got written=%d", bs.Written())
	}
	if !bs.IsResolved(id) {
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

// TestReassessGmailThread_UsesFetchSeamNoNetwork verifies reassessGmailThread drives
// the fetch through the fetchGmailThread seam only -- no real Gmail client call is
// reachable in tests since svc is nil here.
func TestReassessGmailThread_UsesFetchSeamNoNetwork(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenGmailTask(ctx, t, "u@x", "t1", "gmail-src", "Someone Else", time.Now().Add(-time.Hour))

	origFetch := fetchGmailThread
	defer func() { fetchGmailThread = origFetch }()
	fetchGmailThread = func(ctx context.Context, svc *gmail.Service, threadID string) (*gmail.Thread, error) {
		return &gmail.Thread{Messages: []*gmail.Message{
			gmailMsg("src", 0, "a@x.com", "source"),
			gmailMsg("r1", 1, "Counterparty <cp@x.com>", "no completion keyword here"),
		}}, nil
	}

	fa := &fakeAI{transition: ai.TaskTransition{Status: "NONE"}}
	bs := backlog.NewStore(&services.DefaultTaskStore{}, store.GetDB(), false, "gmail")
	svc := services.NewCompletionService(fa, bs, &services.TasksService{}, store.GetDB())

	task := store.ConsolidatedMessage{ID: id, Task: "still open task", Assignee: "Someone Else", Room: "Gmail", ThreadID: "t1", SourceTS: "gmail-src"}
	group := gmailTaskGroup{threadID: "t1", tasks: []store.ConsolidatedMessage{task}}

	reassessGmailThread(ctx, nil, svc, bs, group)

	if fa.callCount != 1 {
		t.Fatalf("expected EvaluateTaskTransition called once for the single candidate reply, got %d", fa.callCount)
	}
}
