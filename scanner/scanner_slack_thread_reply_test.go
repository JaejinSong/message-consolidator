package scanner

import (
	"context"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"message-consolidator/ai"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// fakeThreadReplyAI is a minimal services.AICompleter fake so dispatchCounterpartyThreadReply
// tests can exercise the real *services.CompletionService without a network call.
type fakeThreadReplyAI struct {
	transition ai.TaskTransition
	callCount  int
}

func (f *fakeThreadReplyAI) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	return nil, nil
}

func (f *fakeThreadReplyAI) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (ai.TaskTransition, error) {
	f.callCount++
	return f.transition, nil
}

func (f *fakeThreadReplyAI) Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error) {
	return nil, nil
}

// insertThreadTaskWithAssignee mirrors insertThreadTask but also sets the assignee
// column, needed to distinguish the hard-close vs confirm-first paths.
func insertThreadTaskWithAssignee(ctx context.Context, t *testing.T, email, threadID, assignee string) store.MessageID {
	t.Helper()
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, assignee, done, is_deleted, created_at)
		 VALUES (?, 'slack', 'room', 'still open task', ?, ?, 0, 0, datetime('now'))`,
		email, threadID, assignee)
	if err != nil {
		t.Fatalf("insertThreadTaskWithAssignee: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return store.MessageID(id)
}

func wireFakeCompletionSvc(t *testing.T, fakeAI *fakeThreadReplyAI) {
	t.Helper()
	orig := deps.completionSvc
	t.Cleanup(func() { deps.completionSvc = orig })
	deps.completionSvc = services.NewCompletionService(fakeAI, &services.DefaultTaskStore{}, &services.TasksService{}, store.GetDB())
}

func counterpartyReply(threadTS, text string) slack.Message {
	return slack.Message{Msg: slack.Msg{Timestamp: "999.000000", ThreadTimestamp: threadTS, User: "UOTHER", Text: text}}
}

// TestDispatchCounterpartyThreadReply_PlainAckEvaluatedWithoutKeyword verifies a plain
// ack (no completion keyword) reaches EvaluateTaskTransition when the thread has an
// open task of its own. Why: production evidence showed the keyword gate silently
// dropped exactly these replies.
func TestDispatchCounterpartyThreadReply_PlainAckEvaluatedWithoutKeyword(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	insertThreadTask(ctx, t, "ack@example.com", "100.000000", false)

	fakeAI := &fakeThreadReplyAI{transition: ai.TaskTransition{Status: "NONE"}}
	wireFakeCompletionSvc(t, fakeAI)

	user := &store.User{Email: "ack@example.com", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "100.000000", UserEmail: "ack@example.com"}
	m := counterpartyReply("100.000000", "Got it. Let me share with customer.")

	dispatchCounterpartyThreadReply(ctx, user, thread, m, "room", "Counterparty", newThreadReplyBudget())

	if fakeAI.callCount == 0 {
		t.Fatal("expected EvaluateTaskTransition to be called for a plain ack with an open thread task")
	}
}

// TestDispatchCounterpartyThreadReply_ResolveByAssigneeHardCloses verifies a RESOLVE
// verdict from the task's own assignee closes the task directly.
func TestDispatchCounterpartyThreadReply_ResolveByAssigneeHardCloses(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertThreadTaskWithAssignee(ctx, t, "assignee@example.com", "200.000000", "Counterparty")

	fakeAI := &fakeThreadReplyAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	wireFakeCompletionSvc(t, fakeAI)

	user := &store.User{Email: "assignee@example.com", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C2", ThreadTS: "200.000000", UserEmail: "assignee@example.com"}
	m := counterpartyReply("200.000000", "Turn on 2-step verification already. Thanks JJ.")

	dispatchCounterpartyThreadReply(ctx, user, thread, m, "room", "Counterparty", newThreadReplyBudget())

	var done int
	row := store.GetDB().QueryRowContext(ctx, `SELECT done FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&done); err != nil {
		t.Fatalf("scan done: %v", err)
	}
	if done != 1 {
		t.Errorf("expected the assignee's RESOLVE to hard-close the task, done=%d", done)
	}
}

// TestDispatchCounterpartyThreadReply_ResolveByOtherIsCandidateOnly verifies a RESOLVE
// verdict from someone other than the assignee only records a confirm-first candidate.
func TestDispatchCounterpartyThreadReply_ResolveByOtherIsCandidateOnly(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertThreadTaskWithAssignee(ctx, t, "other@example.com", "300.000000", "Someone Else")

	fakeAI := &fakeThreadReplyAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	wireFakeCompletionSvc(t, fakeAI)

	user := &store.User{Email: "other@example.com", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C3", ThreadTS: "300.000000", UserEmail: "other@example.com"}
	m := counterpartyReply("300.000000", "Correct. But this is old dynatrace RCA")

	dispatchCounterpartyThreadReply(ctx, user, thread, m, "room", "Counterparty", newThreadReplyBudget())

	metadata := readMessageMetadata(ctx, t, id)
	if !strings.Contains(metadata, `"status":"pending"`) {
		t.Errorf("expected a pending completion candidate, got metadata=%s", metadata)
	}
	var done int
	row := store.GetDB().QueryRowContext(ctx, `SELECT done FROM messages WHERE id = ?`, int64(id))
	if err := row.Scan(&done); err != nil {
		t.Fatalf("scan done: %v", err)
	}
	if done != 0 {
		t.Error("expected the task to stay open (confirm-first, not hard-closed)")
	}
}

// TestDispatchCounterpartyThreadReply_DismissedCandidateSkipped verifies a previously
// dismissed candidate source is not re-recorded.
func TestDispatchCounterpartyThreadReply_DismissedCandidateSkipped(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertThreadTaskWithAssignee(ctx, t, "dismissed@example.com", "400.000000", "Someone Else")
	link := "https://slack.com/archives/C4/p999000000?thread_ts=400.000000"
	if _, err := store.GetDB().ExecContext(ctx,
		`UPDATE messages SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.completion_dismissed_source', ?) WHERE id = ?`,
		link, int64(id)); err != nil {
		t.Fatalf("seed dismissed marker: %v", err)
	}

	fakeAI := &fakeThreadReplyAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	wireFakeCompletionSvc(t, fakeAI)

	user := &store.User{Email: "dismissed@example.com", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C4", ThreadTS: "400.000000", UserEmail: "dismissed@example.com"}
	m := counterpartyReply("400.000000", "already resolved")

	dispatchCounterpartyThreadReply(ctx, user, thread, m, "room", "Counterparty", newThreadReplyBudget())

	metadata := readMessageMetadata(ctx, t, id)
	if strings.Contains(metadata, `"status":"pending"`) {
		t.Errorf("expected the dismissed source to stay suppressed, got metadata=%s", metadata)
	}
}

// TestDispatchCounterpartyThreadReply_CapHonoured verifies a fully-used budget skips
// evaluation entirely, without calling EvaluateTaskTransition.
func TestDispatchCounterpartyThreadReply_CapHonoured(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	insertThreadTask(ctx, t, "capped@example.com", "500.000000", false)

	fakeAI := &fakeThreadReplyAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	wireFakeCompletionSvc(t, fakeAI)

	budget := newThreadReplyBudget()
	budget.used = maxThreadReplyEvalsPerSweep

	user := &store.User{Email: "capped@example.com", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C5", ThreadTS: "500.000000", UserEmail: "capped@example.com"}
	m := counterpartyReply("500.000000", "all done here")

	dispatchCounterpartyThreadReply(ctx, user, thread, m, "room", "Counterparty", budget)

	if fakeAI.callCount != 0 {
		t.Errorf("expected the cap to block the call, got callCount=%d", fakeAI.callCount)
	}
}

// TestDispatchCounterpartyThreadReply_NoOpenTaskFallsBackToKeywordGate verifies a
// thread with no open task of its own still uses the keyword-gated cross-channel path.
func TestDispatchCounterpartyThreadReply_NoOpenTaskFallsBackToKeywordGate(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()

	fakeAI := &fakeThreadReplyAI{transition: ai.TaskTransition{Status: "NONE"}}
	wireFakeCompletionSvc(t, fakeAI)

	user := &store.User{Email: "nokeyword@example.com", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C6", ThreadTS: "600.000000", UserEmail: "nokeyword@example.com"}
	m := counterpartyReply("600.000000", "how's the weather today?")

	dispatchCounterpartyThreadReply(ctx, user, thread, m, "room", "Counterparty", newThreadReplyBudget())

	if fakeAI.callCount != 0 {
		t.Error("expected no AI call: no open task in thread and no completion keyword")
	}
}
