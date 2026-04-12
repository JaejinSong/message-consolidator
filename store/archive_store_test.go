package store

import (
	"message-consolidator/internal/testutil"
	"context"
	"testing"
	"time"
)

func TestGetArchivedMessagesFiltered_Status(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()
	defer ResetForTest()

	email := "archive_test@example.com"
	_, err = GetOrCreateUser(context.Background(), email, "Archive User", "")
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// --- Seed Data ---
	now := time.Now().UTC()
	tenDaysAgo := now.AddDate(0, 0, -10)
	//Why: Seeds a task completed 10 days ago to ensure it appears in the archive based on the default auto-archive threshold.
	_, err = GetDB().Exec(`INSERT INTO messages (user_email, task, source, source_ts, pinned, done, is_deleted, completed_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Done Task", "slack", "ts_done", false, true, false, tenDaysAgo, tenDaysAgo)
	if err != nil {
		t.Fatalf("Failed to insert done task: %v", err)
	}

	// 2. Trashed task (done=false, is_deleted=true)
	_, err = GetDB().Exec(`INSERT INTO messages (user_email, task, source, source_ts, pinned, done, is_deleted, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Trashed Task 1", "slack", "ts_trash1", false, false, true, now)
	if err != nil {
		t.Fatalf("Failed to insert trashed task 1: %v", err)
	}

	// 3. Done and Trashed task (done=true, is_deleted=true)
	_, err = GetDB().Exec(`INSERT INTO messages (user_email, task, source, source_ts, pinned, done, is_deleted, completed_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Trashed Task 2", "slack", "ts_trash2", false, true, true, now, now)
	if err != nil {
		t.Fatalf("Failed to insert trashed task 2: %v", err)
	}

	// 4. Active task (should not appear in archive)
	_, err = GetDB().Exec(`INSERT INTO messages (user_email, task, source, source_ts, pinned, done, is_deleted, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Active Task", "slack", "ts_active", false, false, false, now)
	if err != nil {
		t.Fatalf("Failed to insert active task: %v", err)
	}

	// --- Test Cases ---
	testCases := []struct {
		name         string
		statusFilter string
		query        string
		limit        int
		offset       int
		expectedLen  int
		expectedPage int
	}{
		{
			name:         "Status All",
			statusFilter: "all",
			limit:        10,
			expectedLen:  3, // Done + Trashed1 + Trashed2
		},
		{
			name:         "Status Default (empty)",
			statusFilter: "",
			limit:        10,
			expectedLen:  3, // Should default to "all"
		},
		{
			name:         "Status Done",
			statusFilter: "done",
			limit:        10,
			expectedLen:  2, // ID 1 (Done) + ID 3 (Done & Deleted)
		},
		{
			name:         "Status Canceled",
			statusFilter: "canceled",
			limit:        10,
			expectedLen:  1, // ID 2 (Only Deleted, not Done)
		},
		{
			name:         "Status Canceled with Query (Matched)",
			statusFilter: "canceled",
			query:        "Task 1",
			limit:        10,
			expectedLen:  1,
		},
		{
			name:         "Status Canceled with Query (Unmatched)",
			statusFilter: "canceled",
			query:        "Done",
			limit:        10,
			expectedLen:  0,
		},
		{
			name:         "Status All with Pagination",
			statusFilter: "all",
			limit:        1,
			offset:       0,
			expectedLen:  3, // Filtered total
			expectedPage: 1, // Number of items in current page
		},
		{
			name:         "Invalid Status (Fallback to All)",
			statusFilter: "invalid_status_abc",
			limit:        10,
			expectedLen:  3,
			expectedPage: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			l := tc.limit
			if l == 0 {
				l = 10
			}
			filter := ArchiveFilter{
				Email:  email,
				Query:  tc.query,
				Limit:  l,
				Offset: tc.offset,
				Status: tc.statusFilter,
			}
			msgs, total, err := GetArchivedMessagesFiltered(context.Background(), filter)
			if err != nil {
				t.Fatalf("GetArchivedMessagesFiltered failed: %v", err)
			}

			//Why: Verifies that the total count of filtered messages matches the expected dataset size regardless of pagination.
			if total != tc.expectedLen {
				t.Errorf("Expected total count to be %d, but got %d (Filter: %s, Query: %s)", tc.expectedLen, total, tc.statusFilter, tc.query)
			}

			//Why: Verifies that the number of messages returned in the current page respects the requested limit and total available items.
			expectedPage := tc.expectedPage
			if expectedPage == 0 && tc.expectedLen > 0 {
				expectedPage = tc.expectedLen
				if tc.limit > 0 && expectedPage > tc.limit {
					expectedPage = tc.limit
				}
			}
			if len(msgs) != expectedPage {
				t.Errorf("Expected messages length to be %d, but got %d (Filter: %s)", expectedPage, len(msgs), tc.name)
			}
		})
	}
}
