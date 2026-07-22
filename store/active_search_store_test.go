package store

import (
	"context"
	"message-consolidator/internal/testutil"
	"testing"
)

func TestSearchActiveMessages_NoResults(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	results, err := SearchActiveMessages(context.Background(), "nobody@example.com", "hello")
	if err != nil {
		t.Fatalf("SearchActiveMessages: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestSearchOpenTasksFTS exercises the messages_fts triggers end-to-end: only
// lifecycle='active' rows return, multi-token hits outrank single-token ones,
// and guard/escaping edge cases stay error-free.
func TestSearchOpenTasksFTS(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("openfts")

	insert := func(taskText string, done int) MessageID {
		t.Helper()
		completedAt := "NULL"
		if done == 1 {
			completedAt = "datetime('now')"
		}
		res, err := GetDB().ExecContext(ctx,
			`INSERT INTO messages (user_email, task, source, source_ts, original_text, done, completed_at)
			 VALUES (?, ?, 'slack', ?, ?, ?, `+completedAt+`)`,
			email, taskText, testutil.RandomTS(taskText), "body of "+taskText, done)
		if err != nil {
			t.Fatalf("insert msg: %v", err)
		}
		id, _ := res.LastInsertId()
		return MessageID(id)
	}

	openBoth := insert("deploy pricing dashboard", 0)
	openOne := insert("review pricing policy", 0)
	archived := insert("deploy pricing rollback", 1)
	_ = insert("unrelated meeting notes", 0)

	got, err := SearchOpenTasksFTS(ctx, email, []string{"deploy", "pricing", "dashboard"}, 5)
	if err != nil {
		t.Fatalf("SearchOpenTasksFTS: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 open matches, got %d", len(got))
	}
	if got[0].ID != openBoth {
		t.Errorf("BM25 order: want id=%d first (multi-token hit), got %d", openBoth, got[0].ID)
	}
	if got[1].ID != openOne {
		t.Errorf("second: want id=%d, got %d", openOne, got[1].ID)
	}
	for _, m := range got {
		if m.ID == archived {
			t.Errorf("archived row %d must not be returned", archived)
		}
	}

	// Limit is honored.
	if got, err = SearchOpenTasksFTS(ctx, email, []string{"pricing"}, 1); err != nil || len(got) != 1 {
		t.Errorf("limit 1: len=%d err=%v", len(got), err)
	}

	// Guards return nil without touching the DB.
	if got, _ := SearchOpenTasksFTS(ctx, email, nil, 5); got != nil {
		t.Errorf("empty tokens should return nil, got %v", got)
	}
	if got, _ := SearchOpenTasksFTS(ctx, email, []string{"pricing"}, 0); got != nil {
		t.Errorf("zero limit should return nil, got %v", got)
	}

	// Double quotes are escaped, not treated as FTS syntax.
	if _, err := SearchOpenTasksFTS(ctx, email, []string{`pri"cing`}, 5); err != nil {
		t.Errorf("quoted token should not error: %v", err)
	}

	// Other users' rows are invisible.
	if got, err := SearchOpenTasksFTS(ctx, testutil.RandomEmail("other"), []string{"pricing"}, 5); err != nil || len(got) != 0 {
		t.Errorf("cross-user leak: len=%d err=%v", len(got), err)
	}
}
