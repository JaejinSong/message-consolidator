package store

import (
	"testing"
	"time"
)

func TestGetUserStats_IncludesArchived(t *testing.T) {
	cleanup, err := SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()
	defer ResetForTest()

	email := "stats@example.com"
	_, _ = GetOrCreateUser(email, "Stats User", "")

	//Why: 1. Create a message that is DONE and ARCHIVED (is_deleted=1) to verify stats inclusion.
	twoHoursAgo := time.Now().UTC().Add(-2 * time.Hour)
	t1 := twoHoursAgo.Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, is_deleted, completed_at, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Archived Task", "slack", "ts_archived", 1, 1, t1, t1)
	if err != nil {
		t.Fatalf("Failed to insert archived task: %v", err)
	}

	//Why: 2. Create a message that is DONE and ACTIVE (is_deleted=0) to verify multi-state aggregation.
	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	t2 := oneHourAgo.Format(time.RFC3339)
	_, err = db.Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, is_deleted, completed_at, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Active Task", "slack", "ts_active", 1, 0, t2, t2)
	if err != nil {
		t.Fatalf("Failed to insert active task: %v", err)
	}

	//Why: 3. Create a message that is NOT DONE and ACTIVE (Pending) to verify backlog counting.
	_, err = db.Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, is_deleted, created_at, assignee) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Pending Task", "slack", "ts_pending", 0, 0, time.Now(), "me")
	if err != nil {
		t.Fatalf("Failed to insert pending task: %v", err)
	}

	//Why: Triggers GetUserStats across all states to validate the consolidated report.
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

	//Why: 4. Source Distribution checks to ensure per-channel metrics are calculated correctly.
	// Active: Active Task + Pending Task = 2
	if stats.SourceDistribution["slack"] != 2 {
		t.Errorf("Expected SourceDistribution[slack]=2, got %d", stats.SourceDistribution["slack"])
	}
	// Total: Archived + Active + Pending = 3
	if stats.SourceDistributionTotal["slack"] != 3 {
		t.Errorf("Expected SourceDistributionTotal[slack]=3, got %d", stats.SourceDistributionTotal["slack"])
	}
}
