package tests

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
	"testing"
	"time"
)

func TestTaskContextAndRouting(t *testing.T) {
	//Why: Adapts ResetForTest to the required no-op signature for SetupTestDB's resetFunc parameter.
	resetWrapper := func() { store.ResetForTest() }
	cleanup, err := testutil.SetupTestDB(store.InitDB, resetWrapper)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("e2e-logic")
	room := testutil.RandomID("room")
	source := "whatsapp"

	// 1. Create a task (New)
	msg1 := store.ConsolidatedMessage{
		UserEmail:  email,
		Source:     source,
		Room:       room,
		Task:       "Buy milk",
		AssignedAt: time.Now(),
		SourceTS:   testutil.RandomID("ts"),
	}
	id1, err := services.HandleTaskState(context.Background(), nil, email, store.TodoItem{State: "new"}, msg1)
	if err != nil || id1 == 0 {
		t.Fatalf("Failed to create task: %v", err)
	}

	// 2. Verify it shows up in context
	ctxTasks, err := store.GetActiveContextTasks(context.Background(), store.GetDB(), email, source, room)
	if err != nil {
		t.Fatalf("Failed to get context tasks: %v", err)
	}
	if len(ctxTasks) != 1 {
		t.Errorf("Expected 1 task in context, got %d", len(ctxTasks))
	}

	// 3. Update the task (Update/Rewrite)
	updatedTask := "Buy milk and bread"
	id2, err := services.HandleTaskState(context.Background(), nil, email, store.TodoItem{
		ID:    &id1,
		State: "update",
		Task:  updatedTask,
	}, msg1)
	if err != nil || id2 != id1 {
		t.Fatalf("Failed to update task: %v", err)
	}

	// 4. Resolve the task (Resolve)
	_, err = services.HandleTaskState(context.Background(), nil, email, store.TodoItem{
		ID:    &id1,
		State: "resolve",
	}, msg1)
	if err != nil {
		t.Fatalf("Failed to resolve task: %v", err)
	}

	// 5. Verify it is STILL in context after resolve (as a recently completed task)
	ctxTasksAfter, err := store.GetActiveContextTasks(context.Background(), store.GetDB(), email, source, room)
	if err != nil {
		t.Fatalf("Failed to get context tasks after resolve: %v", err)
	}
	if len(ctxTasksAfter) != 1 {
		t.Errorf("Expected 1 task in context after resolve, got %d", len(ctxTasksAfter))
	} else if !ctxTasksAfter[0].Done {
		t.Error("Expected task in context to be marked as Done")
	}
}
