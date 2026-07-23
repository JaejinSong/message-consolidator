package services

import (
	"context"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

func seedThreadParent(t *testing.T, email, room, threadID, task string) store.MessageID {
	t.Helper()
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, thread_id, done, is_deleted) VALUES (?, 'whatsapp', ?, ?, 'orig', ?, ?, 0, 0)",
		email, room, task, threadID, threadID,
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	return store.MessageID(id64)
}

// Why: with WA thread anchors live, a quote-reply proposing an OFF-TOPIC new task must
// not rename the quoted task (updateThreadParentIfPresent overwrites the title) — it
// creates its own task and the parent stays intact.
func TestUpdateThreadParent_OffTopicQuoteCreatesNewTask(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "thread-parent-offtopic@example.com"
	room := "[Technical] Skyworx x WhaTap"
	parentID := seedThreadParent(t, email, room, "3EB0ORIGIN", "Implement APM in SAMCO environment")

	item := store.TodoItem{State: "new", Task: "Share Indofood PO with the team"}
	msg := store.ConsolidatedMessage{
		UserEmail: email, Source: "whatsapp", Room: room,
		SourceTS: "3EB0QUOTEREPLY", ThreadID: "3EB0ORIGIN",
		OriginalText: "bisa tolong share PO yang dari Indofood kah?",
		Task:         "Share Indofood PO with the team",
	}

	gotID, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if gotID == parentID {
		t.Fatal("off-topic quote reply must not update the thread parent")
	}

	var parentTask string
	if err := store.GetDB().QueryRow("SELECT task FROM messages WHERE id = ?", parentID).Scan(&parentTask); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if parentTask != "Implement APM in SAMCO environment" {
		t.Errorf("parent task = %q, want unchanged", parentTask)
	}

	var cnt int
	_ = store.GetDB().QueryRow("SELECT COUNT(*) FROM messages WHERE user_email=? AND task='Share Indofood PO with the team'", email).Scan(&cnt)
	if cnt != 1 {
		t.Errorf("expected the off-topic proposal to become its own task, count=%d", cnt)
	}
}

// Why: a topically tied quote-reply keeps the pre-existing behavior — the thread
// parent absorbs the refined title instead of spawning a duplicate.
func TestUpdateThreadParent_TopicalQuoteUpdatesParent(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "thread-parent-topical@example.com"
	room := "[Technical] Skyworx x WhaTap"
	parentID := seedThreadParent(t, email, room, "3EB0ORIGIN2", "Implement APM in SAMCO environment")

	newTitle := "Implement APM in remaining SAMCO microservices"
	item := store.TodoItem{State: "new", Task: newTitle}
	msg := store.ConsolidatedMessage{
		UserEmail: email, Source: "whatsapp", Room: room,
		SourceTS: "3EB0QUOTEREPLY2", ThreadID: "3EB0ORIGIN2",
		OriginalText: "lanjut pasang di microservice lain ya",
		Task:         newTitle,
	}

	gotID, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if gotID != parentID {
		t.Fatalf("topical quote reply must update the thread parent, got id %d want %d", gotID, parentID)
	}

	var parentTask string
	if err := store.GetDB().QueryRow("SELECT task FROM messages WHERE id = ?", parentID).Scan(&parentTask); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if parentTask != newTitle {
		t.Errorf("parent task = %q, want %q", parentTask, newTitle)
	}
}
