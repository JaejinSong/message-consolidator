package services

import (
	"context"
	"database/sql"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
	"time"
)

// Why (Phase J Path B / J-7): handleUpdate must bump `assigned_at` to the trigger message envelope
// timestamp when assignee changes (e.g. @mention reassignment), and must leave it untouched when
// the assignee is unchanged or absent. These three regression tests pin that contract.

func setupAssignedAtFixture(t *testing.T, room, assignee string, assignedAt time.Time) (string, store.MessageID) {
	t.Helper()
	email := "assigned-at-test@example.com"
	res, err := store.GetDB().Exec(
		"INSERT INTO messages (user_email, source, room, task, assignee, assigned_at, source_ts, done, is_deleted) VALUES (?, 'slack', ?, 'Existing Task', ?, ?, ?, 0, 0)",
		email, room, assignee, assignedAt, "ts-1",
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id64, _ := res.LastInsertId()
	return email, store.MessageID(id64)
}

func readAssignedAt(t *testing.T, email string, id store.MessageID) (string, time.Time) {
	t.Helper()
	var assignee string
	var assignedAt sql.NullTime
	err := store.GetDB().QueryRow(
		"SELECT COALESCE(assignee, ''), assigned_at FROM messages WHERE user_email = ? AND id = ?",
		email, id,
	).Scan(&assignee, &assignedAt)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return assignee, assignedAt.Time
}

func TestHandleUpdate_AssignedAt(t *testing.T) {
	t1 := time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 25, 14, 30, 0, 0, time.UTC)

	cases := []struct {
		name           string
		assignedTo     string
		wantAssignee   string
		wantAssignedAt time.Time
		mismatchMsg    string
	}{
		{
			name:           "BumpsAssignedAtOnAssigneeChange",
			assignedTo:     "Bob",
			wantAssignee:   "Bob",
			wantAssignedAt: t2,
			mismatchMsg:    "assigned_at",
		},
		{
			name:           "AssignedAtNoOpOnSameAssignee",
			assignedTo:     "Alice",
			wantAssignee:   "Alice",
			wantAssignedAt: t1,
			mismatchMsg:    "assigned_at bumped despite no assignee change",
		},
		{
			name:           "AssignedAtPreservedOnEmptyAssignee",
			assignedTo:     "",
			wantAssignee:   "Alice",
			wantAssignedAt: t1,
			mismatchMsg:    "assigned_at bumped despite empty AssignedTo",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			defer cleanup()

			room := "Room-J7"
			email, id := setupAssignedAtFixture(t, room, "Alice", t1)

			idVal := id
			item := store.TodoItem{
				ID:         &idVal,
				State:      "update",
				Task:       "Existing Task",
				AssignedTo: tc.assignedTo,
			}
			msg := store.ConsolidatedMessage{
				UserEmail:  email,
				Source:     "slack",
				Room:       room,
				AssignedAt: t2,
			}

			if _, err := HandleTaskState(context.Background(), nil, email, item, msg); err != nil {
				t.Fatalf("HandleTaskState: %v", err)
			}

			gotAssignee, gotAssignedAt := readAssignedAt(t, email, id)
			if gotAssignee != tc.wantAssignee {
				t.Errorf("assignee = %q, want %q", gotAssignee, tc.wantAssignee)
			}
			if !gotAssignedAt.Equal(tc.wantAssignedAt) {
				t.Errorf("%s: got %v, want %v", tc.mismatchMsg, gotAssignedAt, tc.wantAssignedAt)
			}
		})
	}
}
