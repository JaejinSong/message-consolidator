package services

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
)

// Why: handleCancel used to soft-delete an AI-supplied ID with no room/existence check,
// so a hallucinated ID on the unverified Gmail path could delete an unrelated task. These
// pin the same validateTargetTask gate handleResolve already uses.
func TestHandleCancel(t *testing.T) {
	tests := []struct {
		name        string
		seedRoom    string
		seedDone    bool
		msgRoom     string
		id          *store.MessageID // nil => use seeded id
		wantErr     bool
		wantDeleted bool
	}{
		{
			name:        "same room deletes",
			seedRoom:    "Cancel-Room",
			msgRoom:     "Cancel-Room",
			wantDeleted: true,
		},
		{
			name:        "different room not deleted",
			seedRoom:    "Cancel-Room",
			msgRoom:     "AttackerRoom",
			wantDeleted: false,
		},
		{
			name:        "already-done task in same room still deletes (validateTargetTask has no open-only gate)",
			seedRoom:    "Cancel-Room",
			seedDone:    true,
			msgRoom:     "Cancel-Room",
			wantDeleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			defer cleanup()

			email := "cancel-test@example.com"
			done := 0
			if tt.seedDone {
				done = 1
			}
			res, err := store.GetDB().Exec(
				"INSERT INTO messages (user_email, source, room, task, original_text, source_ts, done, is_deleted) VALUES (?, 'slack', ?, 'T', 'o', 'ts-cancel', ?, 0)",
				email, tt.seedRoom, done,
			)
			if err != nil {
				t.Fatalf("seed: %v", err)
			}
			id64, _ := res.LastInsertId()
			id := store.MessageID(id64)

			idVal := id
			item := store.TodoItem{ID: &idVal, State: "cancel"}
			msg := store.ConsolidatedMessage{
				UserEmail: email,
				Source:    "slack",
				Room:      tt.msgRoom,
			}

			got, err := HandleTaskState(context.Background(), nil, email, item, msg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("HandleTaskState error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != 0 {
				t.Errorf("returned id = %d, want 0", got)
			}

			var isDeleted int
			if err := store.GetDB().QueryRow("SELECT is_deleted FROM messages WHERE id = ?", id).Scan(&isDeleted); err != nil {
				t.Fatalf("read: %v", err)
			}
			gotDeleted := isDeleted != 0
			if gotDeleted != tt.wantDeleted {
				t.Errorf("is_deleted = %d (deleted=%v), want deleted=%v", isDeleted, gotDeleted, tt.wantDeleted)
			}
		})
	}
}

// Why: a hallucinated ID (row does not exist at all) must surface an error and delete
// nothing, matching validateTargetTask's propagation of GetMessageByID's not-found error.
func TestHandleCancel_NonexistentID_NotDeleted(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := "cancel-missing@example.com"
	missingID := store.MessageID(999999)
	item := store.TodoItem{ID: &missingID, State: "cancel"}
	msg := store.ConsolidatedMessage{
		UserEmail: email,
		Source:    "slack",
		Room:      "Cancel-Room",
	}

	got, err := HandleTaskState(context.Background(), nil, email, item, msg)
	if err == nil {
		t.Fatal("HandleTaskState error = nil, want error for nonexistent task")
	}
	if got != 0 {
		t.Errorf("returned id = %d, want 0", got)
	}
}
