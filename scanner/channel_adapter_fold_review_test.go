package scanner

import (
	"context"
	"testing"

	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
)

// foldReviewStubAdapter lets a test pick the source and sender identity on top of the
// WhatsApp adapter's other behavior.
type foldReviewStubAdapter struct {
	whatsAppAdapter
	source string
	fromMe bool
}

func (a foldReviewStubAdapter) Source() string                             { return a.source }
func (a foldReviewStubAdapter) IsFromMe(types.RawMessage, store.User) bool { return a.fromMe }

func TestFoldCandidateParentID(t *testing.T) {
	initTestDB(t)
	const email = "fold-review@example.com"
	const threadID = "thread-fold-1"
	ctx := context.Background()
	if _, err := store.GetOrCreateUser(ctx, email, "Fold User", ""); err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	res, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, requester, assignee, source_ts, thread_id, done, is_deleted)
		 VALUES (?, ?, 'room', 'Send report', 'someone', 'alice', 'ts-parent', ?, 0, 0)`,
		email, store.SourceWhatsApp, threadID)
	if err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	parentID64, _ := res.LastInsertId()

	origSvc := deps.completionSvc
	t.Cleanup(func() { deps.completionSvc = origSvc })

	base := func() (store.TodoItem, store.ConsolidatedMessage, types.RawMessage, foldReviewStubAdapter) {
		return store.TodoItem{State: "new", Assignee: "bob"},
			store.ConsolidatedMessage{ThreadID: threadID},
			types.RawMessage{},
			foldReviewStubAdapter{source: store.SourceWhatsApp}
	}
	tests := []struct {
		name   string
		mutate func(*store.TodoItem, *store.ConsolidatedMessage, *types.RawMessage, *foldReviewStubAdapter)
		noSvc  bool
		want   bool
	}{
		{name: "eligible chat reply returns the open parent", mutate: func(*store.TodoItem, *store.ConsolidatedMessage, *types.RawMessage, *foldReviewStubAdapter) {}, want: true},
		{name: "slack source is skipped", mutate: func(_ *store.TodoItem, _ *store.ConsolidatedMessage, _ *types.RawMessage, a *foldReviewStubAdapter) {
			a.source = store.SourceSlack
		}},
		{name: "gmail source is skipped", mutate: func(_ *store.TodoItem, _ *store.ConsolidatedMessage, _ *types.RawMessage, a *foldReviewStubAdapter) {
			a.source = store.SourceGmail
		}},
		{name: "own message is skipped", mutate: func(_ *store.TodoItem, _ *store.ConsolidatedMessage, _ *types.RawMessage, a *foldReviewStubAdapter) {
			a.fromMe = true
		}},
		{name: "replayed message is skipped", mutate: func(_ *store.TodoItem, _ *store.ConsolidatedMessage, m *types.RawMessage, _ *foldReviewStubAdapter) {
			m.IsReplay = true
		}},
		{name: "non-new state is skipped", mutate: func(i *store.TodoItem, _ *store.ConsolidatedMessage, _ *types.RawMessage, _ *foldReviewStubAdapter) {
			i.State = "update"
		}},
		{name: "empty assignee is skipped", mutate: func(i *store.TodoItem, _ *store.ConsolidatedMessage, _ *types.RawMessage, _ *foldReviewStubAdapter) {
			i.Assignee = ""
		}},
		{name: "message without thread is skipped", mutate: func(_ *store.TodoItem, msg *store.ConsolidatedMessage, _ *types.RawMessage, _ *foldReviewStubAdapter) {
			msg.ThreadID = ""
		}},
		{name: "thread without open parent is skipped", mutate: func(_ *store.TodoItem, msg *store.ConsolidatedMessage, _ *types.RawMessage, _ *foldReviewStubAdapter) {
			msg.ThreadID = "other-thread"
		}},
		{name: "nil completion service is skipped", noSvc: true, mutate: func(*store.TodoItem, *store.ConsolidatedMessage, *types.RawMessage, *foldReviewStubAdapter) {}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			item, msg, raw, adapter := base()
			tc.mutate(&item, &msg, &raw, &adapter)
			msg.UserEmail = email
			deps.completionSvc = nil
			if !tc.noSvc {
				deps.completionSvc = services.NewCompletionService(nil, &services.DefaultTaskStore{}, nil, nil)
			}

			got := foldCandidateParentID(ctx, store.User{Email: email}, item, msg, raw, adapter)

			want := store.MessageID(0)
			if tc.want {
				want = store.MessageID(parentID64)
			}
			if got != want {
				t.Errorf("foldCandidateParentID = %d, want %d", got, want)
			}
		})
	}
}
