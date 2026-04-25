package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

func TestRouteTaskByStatus_Resolve(t *testing.T) {
	// Why: Use testutil.SetupTestDB instead of hardcoded in-memory DSN.
	// modernc.org/sqlite ignores cache=shared, causing empty DB per connection.
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	db := store.GetDB()
	ctx := context.Background()

	email := "test@example.com"
	room := "General"

	// 1. Create a dummy task
	taskID, err := db.Exec("INSERT INTO messages (user_email, source, room, task, category, done) VALUES (?, 'slack', ?, 'Existing Task', 'TASK', 0)", email, room)
	if err != nil {
		t.Fatalf("failed to insert dummy task: %v", err)
	}
	id64, _ := taskID.LastInsertId()
	id := int(id64)

	// 2. Mock AI returning 'resolve' status for this ID
	item := store.TodoItem{
		ID:     &id,
		Status: "resolve",
		Task:   "Existing Task",
	}
	msg := store.ConsolidatedMessage{
		UserEmail: email,
		Source:    "slack",
		Room:      room,
	}

	// 3. Execute Routing
	resID, err := RouteTaskByStatus(ctx, nil, email, item, msg)
	if err != nil {
		t.Fatalf("RouteTaskByStatus failed: %v", err)
	}

	if resID != id {
		t.Errorf("expected resID %d, got %d", id, resID)
	}

	// 4. Verify DB state (should be done)
	var done int
	row := db.QueryRow("SELECT done FROM messages WHERE id = ?", id)
	err = row.Scan(&done)
	if err != nil {
		t.Fatalf("failed to query task status: %v", err)
	}

	if done != 1 {
		t.Errorf("expected task %d to be done, but it is not", id)
	}

	// 5. Verify no new records were created (count should still be 1)
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query message count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 record in DB, got %d (potential duplicate insert)", count)
	}
}

func TestHandleTaskState_PromiseResolvesExistingTask(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	db := store.GetDB()
	email := "test@example.com"
	room := "Project Chat"

	res, _ := db.Exec("INSERT INTO messages (user_email, source, room, task, category, done) VALUES (?, 'whatsapp', ?, 'Submit the report', 'TASK', 0)", email, room)
	id64, _ := res.LastInsertId()
	id := int(id64)

	item := store.TodoItem{
		ID:       &id,
		Status:   "resolve",
		Category: "PROMISE",
		Task:     "I will submit the report",
	}
	msg := store.ConsolidatedMessage{UserEmail: email, Source: "whatsapp", Room: room}

	resID, err := HandleTaskState(ctx, nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if resID != id {
		t.Errorf("expected resID %d, got %d", id, resID)
	}

	var done int
	_ = db.QueryRow("SELECT done FROM messages WHERE id = ?", id).Scan(&done)
	if done != 1 {
		t.Errorf("expected task %d done=1, got %d", id, done)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	if count != 1 {
		t.Errorf("PROMISE should not insert a new record, got %d records", count)
	}
}

func TestHandleTaskState_NewConsolidatesExistingThreadTask(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	db := store.GetDB()
	email := "test@example.com"
	threadID := "thread_ifc_001"

	res, _ := db.Exec(
		"INSERT INTO messages (user_email, source, room, task, thread_id, done) VALUES (?, 'gmail', 'Gmail', 'IFC 기술 미팅 일정 확인', ?, 0)",
		email, threadID,
	)
	existingID64, _ := res.LastInsertId()
	existingID := int(existingID64)

	item := store.TodoItem{
		State: "new",
		Task:  "IFC 말레이시아 온사이트 기술 지원 및 5월 5-6일 미팅 참여 범위 확정",
	}
	msg := store.ConsolidatedMessage{
		UserEmail: email, Source: "gmail", Room: "Gmail", ThreadID: threadID,
	}

	retID, err := HandleTaskState(ctx, nil, email, item, msg)
	if err != nil {
		t.Fatalf("HandleTaskState: %v", err)
	}
	if retID != existingID {
		t.Errorf("expected existing task %d to be updated, got retID=%d", existingID, retID)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 record (no duplicate), got %d", count)
	}

	var task string
	_ = db.QueryRow("SELECT task FROM messages WHERE id = ?", existingID).Scan(&task)
	if task != item.Task {
		t.Errorf("expected task to be updated to %q, got %q", item.Task, task)
	}
}

func TestRouteTaskByStatus_New(t *testing.T) {
	ctx := context.Background()
	email := "test@example.com"

	item := store.TodoItem{
		Status: "new",
		Task:   "New Task",
	}
	msg := store.ConsolidatedMessage{
		UserEmail: email,
	}

	resID, err := RouteTaskByStatus(ctx, nil, email, item, msg)
	if err != nil {
		t.Fatalf("RouteTaskByStatus failed: %v", err)
	}

	if resID != 0 {
		t.Errorf("expected resID 0 for 'new' status, got %d", resID)
	}
}
