package store

import (
	"testing"
	"time"
)

func TestGetUserStats_IncludesArchived(t *testing.T) {
	SetupTestDB()
	defer ResetForTest()

	email := "stats@example.com"
	_, _ = GetOrCreateUser(email, "Stats User", "")

	// 1. Create a message that is DONE and ARCHIVED (is_deleted=1)
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	t1 := twoHoursAgo.Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, is_deleted, completed_at, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Archived Task", "slack", "ts_archived", 1, 1, t1, t1)
	if err != nil {
		t.Fatalf("Failed to insert archived task: %v", err)
	}

	// 2. Create a message that is DONE and ACTIVE (is_deleted=0)
	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	t2 := oneHourAgo.Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, is_deleted, completed_at, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Active Task", "slack", "ts_active", 1, 0, t2, t2)
	if err != nil {
		t.Fatalf("Failed to insert active task: %v", err)
	}

	// 3. Create a message that is NOT DONE and ACTIVE (Pending)
	_, err = db.Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, is_deleted, created_at, assignee) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Pending Task", "slack", "ts_pending", 0, 0, time.Now(), "me")
	if err != nil {
		t.Fatalf("Failed to insert pending task: %v", err)
	}

	// Call GetUserStats
	stats, err := GetUserStats(email, "UTC")
	if err != nil {
		t.Fatalf("GetUserStats failed: %v", err)
	}

	// VERIFICATIONS
	// TotalCompleted should be 2 (Archived + Active)
	if stats.TotalCompleted != 2 {
		t.Errorf("Expected TotalCompleted=2, got %d", stats.TotalCompleted)
	}

	// PendingMe should be 1
	if stats.PendingMe != 1 {
		t.Errorf("Expected PendingMe=1, got %d", stats.PendingMe)
	}

	// HourlyActivity should have 2 entries
	count := 0
	for _, val := range stats.HourlyActivity {
		count += val
	}
	if count != 2 {
		t.Errorf("Expected 2 tasks in HourlyActivity, got %d", count)
	}

	// Check if both hours are recorded
	h1 := oneHourAgo.UTC().Hour()
	h2 := twoHoursAgo.UTC().Hour()
	if stats.HourlyActivity[h1] == 0 {
		t.Errorf("Expected activity at hour %d", h1)
	}
	if stats.HourlyActivity[h2] == 0 {
		t.Errorf("Expected activity at hour %d", h2)
	}
}
