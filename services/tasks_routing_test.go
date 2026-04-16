package services

import (
	"context"
	"message-consolidator/config"
	"message-consolidator/store"
	"testing"
)

func TestRouteTaskByStatus_Resolve(t *testing.T) {
	// Initialize in-memory DB for logic verification
	store.InitDB(&config.Config{TursoURL: "file::memory:?cache=shared"})
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
	resID, err := store.RouteTaskByStatus(ctx, nil, email, item, msg)
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

	resID, err := store.RouteTaskByStatus(ctx, nil, email, item, msg)
	if err != nil {
		t.Fatalf("RouteTaskByStatus failed: %v", err)
	}

	if resID != 0 {
		t.Errorf("expected resID 0 for 'new' status, got %d", resID)
	}
}
