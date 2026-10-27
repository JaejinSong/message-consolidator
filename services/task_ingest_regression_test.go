package services

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func setupIngestRegressionDB(t *testing.T) *store.User {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup database: %v", err)
	}
	t.Cleanup(cleanup)
	user, err := store.GetOrCreateUser(t.Context(), "ingest-regression@example.com", "Owner", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func TestGuardAndBuild_RoutingPreservesResolvedPeople(t *testing.T) {
	cases := []struct {
		name         string
		ccOnly       bool
		aiAssignee   string
		wantAssignee string
	}{
		{name: "cc_only_self_stays_shared", ccOnly: true, aiAssignee: "Owner", wantAssignee: AssigneeShared},
		{name: "metadata_requester_stays_authoritative", aiAssignee: AssigneeShared, wantAssignee: AssigneeShared},
		{name: "ordinary_ai_assignee_is_preserved", aiAssignee: "Owner", wantAssignee: "Owner"},
		{name: "ordinary_other_ai_assignee_is_preserved", aiAssignee: "Delegate", wantAssignee: "Delegate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := setupIngestRegressionDB(t)
			item, msg, guard, ok := GuardAndBuild(t.Context(), TaskBuildParams{
				UserEmail: user.Email, User: *user, Source: "gmail", Room: "Inbox",
				SenderRaw: "Metadata Sender", OriginalText: "Please send the report tomorrow",
				SourceTS: "routing-regression", Timestamp: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
				IsCcOnly: tc.ccOnly, ToHeader: "Delegate <delegate@example.com>",
				Item: store.TodoItem{State: "new", Task: "Send report", Category: "TASK", Requester: "AI Requester", Assignee: tc.aiAssignee},
			})
			if !ok {
				t.Fatalf("GuardAndBuild dropped fixture: %+v", guard)
			}
			if msg.Assignee != tc.wantAssignee || msg.Requester != "Metadata Sender" {
				t.Fatalf("builder fixture: assignee=%q requester=%q", msg.Assignee, msg.Requester)
			}
			id, err := HandleTaskState(t.Context(), store.GetDB(), user.Email, item, msg)
			if err != nil {
				t.Fatalf("route task: %v", err)
			}
			var assignee, requester string
			if err := store.GetDB().QueryRowContext(t.Context(), "SELECT assignee, requester FROM messages WHERE id = ?", id).Scan(&assignee, &requester); err != nil {
				t.Fatalf("read persisted people: %v", err)
			}
			if assignee != tc.wantAssignee {
				t.Errorf("persisted assignee = %q; want builder value %q", assignee, tc.wantAssignee)
			}
			if requester != "Metadata Sender" {
				t.Errorf("persisted requester = %q; want metadata sender", requester)
			}
		})
	}
}

// Why: observe the routed envelope while every read and write uses the real test database.
type recordingIngestStore struct {
	DefaultTaskStore
	messages []store.ConsolidatedMessage
	ids      []store.MessageID
}

func (s *recordingIngestStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	s.messages = append(s.messages, msg)
	id, err := HandleTaskState(ctx, q, email, item, msg)
	if err == nil {
		s.ids = append(s.ids, id)
	}
	return id, err
}

func completionRegressionMessage(user *store.User) store.ConsolidatedMessage {
	return store.ConsolidatedMessage{
		UserEmail: user.Email, Source: "whatsapp", Room: "Project room", Requester: "Envelope Sender", AssigneeReason: "envelope reason",
		Link: "https://example.com/message", ThreadID: "completed-thread", SourceTS: "reply-ts", RepliedToID: "quoted-ts",
		OriginalText: "Please send the report tomorrow and approve the budget. Owner and Envelope Sender are responsible.",
		CreatedAt:    time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), SourceChannels: []string{"whatsapp", "slack"},
		Metadata: json.RawMessage(`{"envelope":"retained","collision":"envelope"}`),
	}
}

func seedCompletedIngestParent(t *testing.T, msg store.ConsolidatedMessage, assignee string) {
	t.Helper()
	_, id, err := store.SaveMessage(t.Context(), store.GetDB(), store.ConsolidatedMessage{
		UserEmail: msg.UserEmail, Source: msg.Source, Room: msg.Room, Task: "Completed deployment",
		ThreadID: msg.ThreadID, SourceTS: "closed-parent-ts", Assignee: assignee, Done: true,
		CreatedAt: msg.CreatedAt.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed completed parent: %v", err)
	}
	if _, err := store.GetDB().ExecContext(t.Context(), "UPDATE messages SET done = 1 WHERE id = ?", id); err != nil {
		t.Fatalf("complete parent: %v", err)
	}
}

func TestCompletionFallback_UsesCommonIngestPolicies(t *testing.T) {
	cases := []struct {
		name             string
		items            []store.TodoItem
		envelopeAssignee string
		parentAssignee   string
		wantAssignees    []string
		wantDeadline     string
		originalText     string
		alias            string
	}{
		{name: "deadline_and_envelope", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK", Deadline: "tomorrow", Assignee: "Owner", Requester: "AI Requester", AssigneeReason: "item reason", Metadata: json.RawMessage(`{"item":"retained","collision":"item"}`)}}, wantAssignees: []string{"Owner"}, wantDeadline: "2026-10-28"},
		{name: "suppressed_items_are_consumed", items: []store.TodoItem{{State: "new", Task: "Rotate certificates", Category: "TASK"}}, wantAssignees: []string{}},
		{name: "multiple_items_keep_only_grounded_tasks", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}, {State: "new", Task: "Rotate certificates", Category: "TASK"}, {State: "new", Task: "Approve budget", Category: "TASK"}}, wantAssignees: []string{AssigneeShared, AssigneeShared}},
		{name: "ai_assignee_precedes_envelope_and_thread", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK", Assignee: "Envelope Sender"}}, envelopeAssignee: "Owner", parentAssignee: "Owner", wantAssignees: []string{"Envelope Sender"}},
		{name: "envelope_assignee_fills_blank_ai", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}, envelopeAssignee: "Owner", parentAssignee: "Envelope Sender", wantAssignees: []string{"Owner"}},
		{name: "latest_thread_assignee_fills_blank_envelope", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}, parentAssignee: "Owner", wantAssignees: []string{"Owner"}},
		{name: "trusted_envelope_assignee_survives_guard", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}, envelopeAssignee: "Previous Owner", wantAssignees: []string{"Previous Owner"}},
		{name: "trusted_thread_assignee_survives_guard", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}, parentAssignee: "Previous Owner", wantAssignees: []string{"Previous Owner"}},
		{name: "group_self_fallback_without_mention_stays_shared", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}, envelopeAssignee: "Owner", originalText: "Please send the report tomorrow.", wantAssignees: []string{AssigneeShared}},
		{name: "group_alias_fallback_without_mention_stays_shared", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}, envelopeAssignee: "Owner Alias", alias: "Owner Alias", originalText: "Please send the report tomorrow.", wantAssignees: []string{AssigneeShared}},
		{name: "explicit_shared_does_not_inherit", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK", Assignee: AssigneeShared}}, parentAssignee: "Owner", wantAssignees: []string{AssigneeShared}},
		{name: "self_assignment_uses_user_profile", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK", Assignee: "me"}}, wantAssignees: []string{"Owner"}},
		{name: "unknown_ai_assignee_is_demoted", items: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK", Assignee: "Hallucinated Person"}}, wantAssignees: []string{AssigneeShared}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user := setupIngestRegressionDB(t)
			msg := completionRegressionMessage(user)
			msg.Assignee = tc.envelopeAssignee
			if tc.originalText != "" {
				msg.OriginalText = tc.originalText
			}
			if tc.alias != "" {
				if _, err := store.UpsertContact(t.Context(), user.Email, user.Email, tc.alias, "", "manual"); err != nil {
					t.Fatalf("add user alias: %v", err)
				}
			}
			if tc.parentAssignee != "" {
				seedCompletedIngestParent(t, msg, tc.parentAssignee)
			}
			taskStore := &recordingIngestStore{}
			mockAI := &MockAI{Results: tc.items}
			svc := NewCompletionService(mockAI, taskStore, &TasksService{}, store.GetDB())
			if !svc.fallbackToNewExtraction(t.Context(), msg) {
				t.Fatal("nonempty extraction must be consumed even when every item is suppressed")
			}
			if mockAI.AnalyzeCallCount != 1 {
				t.Errorf("Analyze calls = %d; want 1", mockAI.AnalyzeCallCount)
			}
			if len(taskStore.messages) != len(tc.wantAssignees) {
				t.Fatalf("routed items = %d; want %d after guards", len(taskStore.messages), len(tc.wantAssignees))
			}
			if len(taskStore.ids) != len(tc.wantAssignees) {
				t.Fatalf("saved items = %d; want %d", len(taskStore.ids), len(tc.wantAssignees))
			}
			var savedRows int
			if err := store.GetDB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM messages WHERE user_email = ? AND done = 0", user.Email).Scan(&savedRows); err != nil {
				t.Fatalf("count saved extractions: %v", err)
			}
			if (savedRows > 0) != (len(tc.wantAssignees) > 0) {
				t.Errorf("saved rows = %d; want rows only when guarded items remain", savedRows)
			}
			for i, routed := range taskStore.messages {
				assertCompletionEnvelope(t, routed, msg)
				if tc.name == "deadline_and_envelope" {
					assertCompletionItemOverrides(t, routed, taskStore.ids[i])
				}
				if routed.Assignee != tc.wantAssignees[i] {
					t.Errorf("routed assignee = %q; want %q", routed.Assignee, tc.wantAssignees[i])
				}
				// Why: source_ts dedup can consume a later item without inserting a separate row.
				if taskStore.ids[i] != 0 {
					assertCompletionPersisted(t, taskStore.ids[i], tc.wantAssignees[i], tc.wantDeadline)
				}
			}
		})
	}
}

func assertCompletionItemOverrides(t *testing.T, msg store.ConsolidatedMessage, id store.MessageID) {
	t.Helper()
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(msg.Metadata, &metadata); err != nil {
		t.Fatalf("decode routed metadata: %v", err)
	}
	if string(metadata["item"]) != `"retained"` || string(metadata["collision"]) != `"item"` {
		t.Errorf("item metadata must be retained and win collisions: %s", msg.Metadata)
	}
	var reason string
	if err := store.GetDB().QueryRowContext(t.Context(), "SELECT assignee_reason FROM messages WHERE id = ?", id).Scan(&reason); err != nil {
		t.Fatalf("read persisted assignee reason: %v", err)
	}
	if reason != "item reason" {
		t.Errorf("persisted assignee reason = %q; want item reason", reason)
	}
}

func assertCompletionEnvelope(t *testing.T, got, want store.ConsolidatedMessage) {
	t.Helper()
	if got.UserEmail != want.UserEmail || got.Source != want.Source || got.Room != want.Room || got.Link != want.Link || got.ThreadID != want.ThreadID || got.SourceTS != want.SourceTS || got.RepliedToID != want.RepliedToID || got.OriginalText != want.OriginalText {
		t.Errorf("routed envelope lost source or user context: %+v", got)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("created timestamp = %v; want envelope timestamp %v", got.CreatedAt, want.CreatedAt)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(got.Metadata, &metadata); err != nil {
		t.Errorf("routed metadata is invalid: %v", err)
	} else if string(metadata["envelope"]) != `"retained"` || len(metadata["ai_original"]) == 0 {
		t.Errorf("routed metadata must retain envelope and guard snapshot: %s", got.Metadata)
	}
	if len(metadata["item"]) > 0 && (string(metadata["item"]) != `"retained"` || string(metadata["collision"]) != `"item"`) {
		t.Errorf("item metadata must win collisions: %s", got.Metadata)
	}
	if got.Requester != want.Requester {
		t.Errorf("routed requester = %q; want envelope requester %q", got.Requester, want.Requester)
	}
	if !got.AssignedAt.Equal(want.CreatedAt) {
		t.Errorf("assigned timestamp = %v; want envelope timestamp %v", got.AssignedAt, want.CreatedAt)
	}
	if !reflect.DeepEqual(got.SourceChannels, want.SourceChannels) {
		t.Errorf("source channels = %v; want %v", got.SourceChannels, want.SourceChannels)
	}
}

func assertCompletionPersisted(t *testing.T, id store.MessageID, wantAssignee, wantDeadline string) {
	t.Helper()
	var assignee, deadline, requester string
	var inferred bool
	err := store.GetDB().QueryRowContext(t.Context(), "SELECT COALESCE(assignee, ''), COALESCE(SUBSTR(deadline_date, 1, 10), ''), COALESCE(deadline_inferred, 0), COALESCE(requester, '') FROM messages WHERE id = ?", id).Scan(&assignee, &deadline, &inferred, &requester)
	if err != nil {
		t.Fatalf("read persisted extraction: %v", err)
	}
	if assignee != wantAssignee {
		t.Errorf("persisted assignee = %q; want %q", assignee, wantAssignee)
	}
	if deadline != wantDeadline || inferred != (wantDeadline != "") {
		t.Errorf("persisted deadline = %q inferred=%v; want %q inferred=%v", deadline, inferred, wantDeadline, wantDeadline != "")
	}
	if requester != "Envelope Sender" {
		t.Errorf("persisted requester = %q; want envelope requester", requester)
	}
}

func TestCompletionFallback_EmptyOrFailedExtractionIsNotConsumed(t *testing.T) {
	for _, tc := range []struct {
		name string
		ai   *MockAI
	}{
		{name: "empty", ai: &MockAI{}},
		{name: "error", ai: &MockAI{Err: errors.New("extraction failed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := setupIngestRegressionDB(t)
			taskStore := &recordingIngestStore{}
			svc := NewCompletionService(tc.ai, taskStore, &TasksService{}, store.GetDB())
			if svc.fallbackToNewExtraction(t.Context(), completionRegressionMessage(user)) {
				t.Fatal("empty or failed extraction must allow normal processing")
			}
			if len(taskStore.messages) != 0 {
				t.Fatal("empty or failed extraction must not route tasks")
			}
		})
	}
}

func TestCompletionFallback_UserContextFailureIsConsumedWithoutRouting(t *testing.T) {
	user := setupIngestRegressionDB(t)
	msg := completionRegressionMessage(user)
	msg.UserEmail = "uncached-user@example.com"
	msg.ThreadID = ""
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	taskStore := &recordingIngestStore{}
	svc := NewCompletionService(&MockAI{Results: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}}, taskStore, &TasksService{}, store.GetDB())
	if !svc.fallbackToNewExtraction(ctx, msg) {
		t.Fatal("successful AI extraction must be consumed after user context loading fails")
	}
	if len(taskStore.messages) != 0 {
		t.Errorf("routed items = %d; want zero without a loaded user context", len(taskStore.messages))
	}
}

func TestCompletionFallback_SaveFailureStillConsumesExtraction(t *testing.T) {
	user := setupIngestRegressionDB(t)
	_, err := store.GetDB().ExecContext(t.Context(), "CREATE TRIGGER reject_fallback_insert BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT, 'simulated fallback save failure'); END")
	if err != nil {
		t.Fatalf("install save failure trigger: %v", err)
	}
	taskStore := &recordingIngestStore{}
	svc := NewCompletionService(&MockAI{Results: []store.TodoItem{{State: "new", Task: "Send report", Category: "TASK"}}}, taskStore, &TasksService{}, store.GetDB())
	if !svc.fallbackToNewExtraction(t.Context(), completionRegressionMessage(user)) {
		t.Fatal("successful AI extraction must be consumed even if saving fails")
	}
	if len(taskStore.messages) != 1 || len(taskStore.ids) != 0 {
		t.Errorf("routed=%d successful saves=%d; want one failed save", len(taskStore.messages), len(taskStore.ids))
	}
	var count int
	if err := store.GetDB().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM messages").Scan(&count); err != nil {
		t.Fatalf("count persisted messages: %v", err)
	}
	if count != 0 {
		t.Errorf("persisted messages = %d; want zero after failed insert", count)
	}
}

var _ TaskStore = (*recordingIngestStore)(nil)
