package services

import (
	"context"
	"errors"
	"testing"

	"message-consolidator/ai"
	"message-consolidator/store"
)

// errHandleTaskStateStore wraps MockStore and forces HandleTaskState to fail,
// proving handleCompletionResult surfaces the write error instead of silently
// marking the message handled.
type errHandleTaskStateStore struct {
	MockStore
}

func (e *errHandleTaskStateStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	return 0, errors.New("simulated write failure")
}

func TestHandleCompletionResult_HandleTaskStateError_ResolveReturnsFalse(t *testing.T) {
	ctx := context.Background()
	mockStore := &errHandleTaskStateStore{}
	svc := NewCompletionService(&MockAI{}, mockStore, &TasksService{}, nil)

	res := ai.TaskTransition{Status: "RESOLVE"}
	parent := store.ConsolidatedMessage{ID: 999, Task: "Some task"}
	msg := store.ConsolidatedMessage{UserEmail: "u@x"}

	if got := svc.handleCompletionResult(ctx, res, msg, parent); got {
		t.Error("expected handleCompletionResult to return false when HandleTaskState fails on RESOLVE")
	}
}

func TestHandleCompletionResult_HandleTaskStateError_UpdateReturnsFalse(t *testing.T) {
	ctx := context.Background()
	mockStore := &errHandleTaskStateStore{}
	svc := NewCompletionService(&MockAI{}, mockStore, &TasksService{}, nil)

	res := ai.TaskTransition{Status: "UPDATE", UpdatedText: "Updated task text"}
	parent := store.ConsolidatedMessage{ID: 1000, Task: "Some task"}
	msg := store.ConsolidatedMessage{UserEmail: "u@x"}

	if got := svc.handleCompletionResult(ctx, res, msg, parent); got {
		t.Error("expected handleCompletionResult to return false when HandleTaskState fails on UPDATE")
	}
}
