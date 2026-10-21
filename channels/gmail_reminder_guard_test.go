package channels

import (
	"context"
	"testing"

	"message-consolidator/store"
)

func TestWithReminderGuard(t *testing.T) {
	tests := []struct {
		name        string
		guard       ReminderGuard
		wantRouted  bool
		wantID      store.MessageID
		wantGuarded int
	}{
		{"nil guard routes normally", nil, true, 7, 0},
		{"reminder short-circuits creation", func(context.Context, store.TodoItem, store.ConsolidatedMessage) bool {
			return true
		}, false, 0, 1},
		{"non-reminder routes normally", func(context.Context, store.TodoItem, store.ConsolidatedMessage) bool {
			return false
		}, true, 7, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			routed := false
			route := func(context.Context, store.Querier, string, store.TodoItem, store.ConsolidatedMessage) (store.MessageID, error) {
				routed = true
				return 7, nil
			}
			guard := tc.guard
			guardCalls := 0
			if guard != nil {
				inner := guard
				guard = func(ctx context.Context, i store.TodoItem, m store.ConsolidatedMessage) bool {
					guardCalls++
					return inner(ctx, i, m)
				}
			}

			id, err := withReminderGuard(guard, route)(context.Background(), nil, "u@x", store.TodoItem{State: "new"}, store.ConsolidatedMessage{})

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if routed != tc.wantRouted || id != tc.wantID || guardCalls != tc.wantGuarded {
				t.Errorf("routed=%v id=%d guardCalls=%d, want %v %d %d", routed, id, guardCalls, tc.wantRouted, tc.wantID, tc.wantGuarded)
			}
		})
	}
}
