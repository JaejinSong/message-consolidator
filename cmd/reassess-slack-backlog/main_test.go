package main

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/scanner"
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

// insertOpenSlackTask inserts a minimal open Slack task row with a thread link, so
// openSlackTasks/groupByThread/evaluateReplyAgainstTask can exercise it end to end.
func insertOpenSlackTask(ctx context.Context, t *testing.T, email, channelID, threadTS, assignee string, assignedAt time.Time) store.MessageID {
	t.Helper()
	link := "https://slack.com/archives/" + channelID + "/p" + threadTS + "?thread_ts=" + threadTS
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, link, assignee, assigned_at, done, is_deleted, created_at)
		 VALUES (?, 'slack', 'room', 'still open task', ?, ?, ?, ?, 0, 0, datetime('now'))`,
		email, threadTS, link, assignee, assignedAt.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("insertOpenSlackTask: %v", err)
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

func TestChannelFromSlackLink(t *testing.T) {
	cases := []struct {
		name string
		link string
		want string
		ok   bool
	}{
		{"well formed", "https://slack.com/archives/C08P27C7AC8/p1789546445856419?thread_ts=1789461410.169779", "C08P27C7AC8", true},
		{"no query", "https://slack.com/archives/C1/p123", "C1", true},
		{"empty", "", "", false},
		{"not a slack link", "https://example.com/foo", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := channelFromSlackLink(c.link)
			if ok != c.ok || got != c.want {
				t.Errorf("channelFromSlackLink(%q) = (%q, %v), want (%q, %v)", c.link, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestOpenSlackTasks_FiltersSourceDoneAndThread(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	insertOpenSlackTask(ctx, t, "u@x", "C1", "100.000000", "", time.Now())
	// done task -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at) VALUES ('u@x', 'slack', 'r', 't', '200.000000', 1, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed done task: %v", err)
	}
	// gmail source -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at) VALUES ('u@x', 'gmail', 'r', 't', 'gm1', 0, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed gmail task: %v", err)
	}
	// no thread_id -> excluded
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, thread_id, done, is_deleted, created_at) VALUES ('u@x', 'slack', 'r', 't', '', 0, 0, datetime('now'))`); err != nil {
		t.Fatalf("seed no-thread task: %v", err)
	}

	tasks, err := openSlackTasks(ctx, "u@x", 151)
	if err != nil {
		t.Fatalf("openSlackTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly 1 open Slack task with a thread, got %d", len(tasks))
	}
	if tasks[0].ThreadID != "100.000000" {
		t.Errorf("ThreadID = %q, want %q", tasks[0].ThreadID, "100.000000")
	}
}

func TestGroupByThread(t *testing.T) {
	link := func(ch, ts string) string { return "https://slack.com/archives/" + ch + "/p" + ts }
	tasks := []store.ConsolidatedMessage{
		{ID: 1, ThreadID: "100", Link: link("C1", "100")},
		{ID: 2, ThreadID: "100", Link: link("C1", "999")}, // same channel+thread, different reply link
		{ID: 3, ThreadID: "200", Link: link("C2", "200")},
		{ID: 4, ThreadID: "300", Link: ""}, // unparseable -> skipped
	}
	groups := groupByThread(tasks)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	total := 0
	for _, g := range groups {
		total += len(g.tasks)
	}
	if total != 3 {
		t.Errorf("expected 3 grouped tasks (1 skipped), got %d", total)
	}
}

func TestIsAccessError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errStr("channel_not_found"), true},
		{errStr("not_in_channel"), true},
		{errStr("some transient timeout"), false},
	}
	for _, c := range cases {
		if got := isAccessError(c.err); got != c.want {
			t.Errorf("isAccessError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }

// TestEvaluateReplyAgainstTask_CounterpartyRESOLVE_recordsCandidate_dryRun verifies the
// tool calls the SAME evaluator as the sweep (EvaluateThreadReply) for a plain reply
// with no completion keyword, and a dry run records the row but writes nothing.
func TestEvaluateReplyAgainstTask_CounterpartyRESOLVE_recordsCandidate_dryRun(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenSlackTask(ctx, t, "u@x", "C1", "100.000000", "Someone Else", time.Now().Add(-time.Hour))

	fa := &fakeAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: false}
	svc := services.NewCompletionService(fa, bs, &services.TasksService{}, store.GetDB())

	task := store.ConsolidatedMessage{ID: id, Task: "still open task", Assignee: "Someone Else"}
	group := slackTaskGroup{channelID: "C1", threadTS: "100.000000"}
	m := slack.Message{Msg: slack.Msg{Timestamp: "150.000000", User: "UOTHER", Text: "already resolved, no keyword here"}}

	evaluateReplyAgainstTask(ctx, svc, bs, group, m, task, "room", "Counterparty")

	if fa.callCount == 0 {
		t.Fatal("expected EvaluateTaskTransition to be called without a completion-signal keyword")
	}
	if len(bs.rows) != 1 {
		t.Fatalf("expected 1 recorded row, got %d", len(bs.rows))
	}
	if bs.written != 0 {
		t.Errorf("dry run must not write, got written=%d", bs.written)
	}
	if bs.rows[0].speaker != "Counterparty" {
		t.Errorf("speaker = %q, want %q (empty speaker silently disables senderIsAssignee)", bs.rows[0].speaker, "Counterparty")
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
	id := insertOpenSlackTask(ctx, t, "u@x", "C2", "200.000000", "Me", time.Now().Add(-time.Hour))

	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: true}
	bs.currentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me"}

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "150.000000"}

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
	id := insertOpenSlackTask(ctx, t, "u@x", "C3", "300.000000", "Me", time.Now().Add(-time.Hour))
	if _, err := store.GetDB().ExecContext(ctx,
		`UPDATE messages SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.completion_dismissed_source', '150.000000') WHERE id = ?`,
		int64(id)); err != nil {
		t.Fatalf("seed dismissed marker: %v", err)
	}

	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: true}
	bs.currentTask = store.ConsolidatedMessage{ID: id, Assignee: "Me", Metadata: []byte(`{"completion_dismissed_source":"150.000000"}`)}

	item := store.TodoItem{State: "resolve", ID: &id}
	msg := store.ConsolidatedMessage{UserEmail: "u@x", Room: "room", Requester: "Me", OriginalText: "done", SourceTS: "150.000000"}

	if _, err := bs.HandleTaskState(ctx, store.GetDB(), "u@x", item, msg); err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if bs.written != 0 {
		t.Errorf("expected the dismissed source to stay suppressed, got written=%d", bs.written)
	}
}

// TestBuildToolEnvelope_MatchesSweepEnvelopeBuilder verifies the tool's envelope
// construction is not a second, divergent implementation: it must delegate to the exact
// same scanner.BuildThreadCompletionEnvelope call the sweep's counterparty-reply path
// (dispatchCounterpartyThreadReply) uses.
func TestBuildToolEnvelope_MatchesSweepEnvelopeBuilder(t *testing.T) {
	task := store.ConsolidatedMessage{UserEmail: "u@x"}
	group := slackTaskGroup{channelID: "C1", threadTS: "100.000000"}
	m := slack.Message{Msg: slack.Msg{Timestamp: "150.000000", User: "UOTHER", Text: "already resolved, no keyword here"}}

	got := buildToolEnvelope(group, task, m, "room", "Counterparty")

	meta := store.SlackThreadMeta{UserEmail: task.UserEmail, ChannelID: group.channelID, ThreadTS: group.threadTS}
	want := scanner.BuildThreadCompletionEnvelope(&store.User{Email: task.UserEmail}, meta, m, "room", "Counterparty", false)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("tool envelope diverged from the shared sweep builder:\n got  %+v\n want %+v", got, want)
	}
}

// TestEvaluateReplyAgainstTask_UnresolvedSenderFallsBackToSlackID_stillRecorded verifies
// that when Slack name resolution fails, channels.SlackClient.GetUserName falls back to
// the raw Slack user ID rather than an empty string -- and the tool records that non-empty
// fallback as the audit row's speaker instead of going blank.
func TestEvaluateReplyAgainstTask_UnresolvedSenderFallsBackToSlackID_stillRecorded(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	id := insertOpenSlackTask(ctx, t, "u@x", "C1", "100.000000", "Someone Else", time.Now().Add(-time.Hour))

	fa := &fakeAI{transition: ai.TaskTransition{Status: "RESOLVE"}}
	bs := &backlogStore{inner: &services.DefaultTaskStore{}, db: store.GetDB(), apply: false}
	svc := services.NewCompletionService(fa, bs, &services.TasksService{}, store.GetDB())

	task := store.ConsolidatedMessage{ID: id, Task: "still open task", Assignee: "Someone Else"}
	group := slackTaskGroup{channelID: "C1", threadTS: "100.000000"}
	m := slack.Message{Msg: slack.Msg{Timestamp: "150.000000", User: "U0UNRESOLVED", Text: "Hmm.. Ok. I'll check and update you."}}
	// Why: mirrors channels.SlackClient.GetUserName's own fallback when users.info
	// fails ("GetUserName resolve failed for id=...") -- it returns the raw Slack
	// user ID, never an empty string.
	fallbackSpeaker := m.User

	evaluateReplyAgainstTask(ctx, svc, bs, group, m, task, "room", fallbackSpeaker)

	if len(bs.rows) != 1 {
		t.Fatalf("expected 1 recorded row, got %d", len(bs.rows))
	}
	if bs.rows[0].speaker == "" {
		t.Fatal("speaker must never be empty, even when name resolution fell back to the raw Slack ID")
	}
	if bs.rows[0].speaker != fallbackSpeaker {
		t.Errorf("speaker = %q, want the resolved fallback %q", bs.rows[0].speaker, fallbackSpeaker)
	}
}

func TestPrintResults_EmptyIsHandled(t *testing.T) {
	bs := &backlogStore{}
	bs.printResults() // must not panic on an empty result set
}
