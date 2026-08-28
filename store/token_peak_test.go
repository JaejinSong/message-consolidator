package store

import (
	"context"
	"testing"
	"time"

	"message-consolidator/internal/testutil"
)

// TestIsPeakWindow pins DeepSeek's published peak schedule: UTC [01:00,04:00) and
// [06:00,10:00) on weekdays only. Boundaries matter because an off-by-one hour doubles or
// halves every row priced in that hour.
func TestIsPeakWindow(t *testing.T) {
	t.Parallel()
	// 2026-08-26 is a Wednesday; 2026-08-29 a Saturday; 2026-08-30 a Sunday.
	cases := []struct {
		name string
		ts   string
		want bool
	}{
		{"weekday just before window A", "2026-08-26T00:59:59Z", false},
		{"weekday window A start", "2026-08-26T01:00:00Z", true},
		{"weekday window A last hour", "2026-08-26T03:59:59Z", true},
		{"weekday window A end is exclusive", "2026-08-26T04:00:00Z", false},
		{"weekday gap between windows", "2026-08-26T05:30:00Z", false},
		{"weekday window B start", "2026-08-26T06:00:00Z", true},
		{"weekday window B last hour", "2026-08-26T09:59:59Z", true},
		{"weekday window B end is exclusive", "2026-08-26T10:00:00Z", false},
		{"weekday late evening", "2026-08-26T22:00:00Z", false},
		{"saturday inside window A", "2026-08-29T02:00:00Z", false},
		{"sunday inside window B", "2026-08-30T07:00:00Z", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts, err := time.Parse(time.RFC3339, tc.ts)
			if err != nil {
				t.Fatalf("parse %s: %v", tc.ts, err)
			}
			if got := isPeakWindow(ts); got != tc.want {
				t.Errorf("isPeakWindow(%s) = %v, want %v", tc.ts, got, tc.want)
			}
		})
	}
}

// TestIsPeakWindowNormalizesToUTC guards the classification against the server's local zone:
// DeepSeek's windows are UTC, so the same instant must classify identically either way.
func TestIsPeakWindowNormalizesToUTC(t *testing.T) {
	t.Parallel()
	utcInstant := time.Date(2026, 8, 26, 2, 0, 0, 0, time.UTC) // inside window A
	kst := time.FixedZone("KST", 9*60*60)
	if !isPeakWindow(utcInstant.In(kst)) {
		t.Error("same instant expressed in KST must still be peak")
	}
	// 11:00 KST == 02:00 UTC; a naive local-hour check would read 11 and miss the window.
	if !isPeakWindow(time.Date(2026, 8, 26, 11, 0, 0, 0, kst)) {
		t.Error("11:00 KST is 02:00 UTC and must be peak")
	}
}

// TestTokenUsageByModelSplitsPeakWindows verifies the v17 grouping: one model used in both
// rate windows comes back as two rows, so costByModel can price each at its own rate.
func TestTokenUsageByModelSplitsPeakWindows(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	conn := GetDB()
	email := testutil.RandomEmail("peaksplit")

	const insert = `INSERT INTO token_usage
		(user_email, date, step, model, source, report_id, peak, prompt_tokens, completion_tokens, thinking_tokens, cached_tokens, total_tokens, call_count)
		VALUES (?, date('now'), 'Analyze', 'deepseek-v4-flash', 'slack', 0, ?, ?, ?, 0, ?, 0, 1)`
	if _, err := conn.Exec(insert, email, 0, 1000, 200, 300); err != nil {
		t.Fatalf("seed off-peak row: %v", err)
	}
	if _, err := conn.Exec(insert, email, 1, 500, 100, 50); err != nil {
		t.Fatalf("seed peak row: %v", err)
	}

	rows, err := GetMonthlyTokenUsageByModel(ctx, email)
	if err != nil {
		t.Fatalf("GetMonthlyTokenUsageByModel: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected one row per rate window, got %d: %+v", len(rows), rows)
	}

	byPeak := make(map[bool]ModelTokenUsage, len(rows))
	for _, r := range rows {
		byPeak[r.Peak] = r
	}
	off, ok := byPeak[false]
	if !ok {
		t.Fatalf("missing off-peak row in %+v", rows)
	}
	if off.Prompt != 1000 || off.Completion != 200 || off.Cached != 300 {
		t.Errorf("off-peak = %+v, want prompt 1000 completion 200 cached 300", off)
	}
	peak, ok := byPeak[true]
	if !ok {
		t.Fatalf("missing peak row in %+v", rows)
	}
	if peak.Prompt != 500 || peak.Completion != 100 || peak.Cached != 50 {
		t.Errorf("peak = %+v, want prompt 500 completion 100 cached 50", peak)
	}
}

// TestMigrateTokenUsagePeak rebuilds a pre-v17 token_usage and checks the three things the
// migration has to get right: existing rows survive as off-peak, the widened UNIQUE lets a
// peak and an off-peak row for the same bucket coexist, and a second pass is a no-op.
func TestMigrateTokenUsagePeak(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	conn := GetDB()
	email := testutil.RandomEmail("peakmigrate")

	// Recreate the pre-v17 shape: no peak column, UNIQUE without it.
	legacy := []string{
		`DROP TABLE token_usage`,
		`CREATE TABLE token_usage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email VARCHAR(255) NOT NULL,
			date DATE NOT NULL DEFAULT (date('now')),
			step TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			report_id INTEGER NOT NULL DEFAULT 0,
			prompt_tokens INT DEFAULT 0,
			completion_tokens INT DEFAULT 0,
			thinking_tokens INT DEFAULT 0,
			cached_tokens INT DEFAULT 0,
			total_tokens INT DEFAULT 0,
			call_count INT DEFAULT 0,
			filtered_count INT DEFAULT 0,
			UNIQUE(user_email, date, step, model, source, report_id)
		)`,
	}
	for _, stmt := range legacy {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("build legacy table: %v", err)
		}
	}
	if _, err := conn.Exec(
		`INSERT INTO token_usage (user_email, date, step, model, source, report_id, prompt_tokens, completion_tokens, cached_tokens, call_count)
		 VALUES (?, date('now'), 'Analyze', 'deepseek-v4-flash', 'slack', 0, 1234, 567, 89, 3)`, email); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	for pass := 1; pass <= 2; pass++ { // second pass proves idempotency
		if err := migrateTokenUsagePeak(ctx, conn); err != nil {
			t.Fatalf("migrate pass %d: %v", pass, err)
		}
	}

	var peak, prompt, completion, cached, calls int
	if err := conn.QueryRow(
		`SELECT peak, prompt_tokens, completion_tokens, cached_tokens, call_count
		 FROM token_usage WHERE user_email = ?`, email).
		Scan(&peak, &prompt, &completion, &cached, &calls); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if peak != 0 {
		t.Errorf("historical row peak = %d, want 0 (window unrecoverable, must not be repriced up)", peak)
	}
	if prompt != 1234 || completion != 567 || cached != 89 || calls != 3 {
		t.Errorf("migrated counters = %d/%d/%d/%d, want 1234/567/89/3", prompt, completion, cached, calls)
	}

	// The widened UNIQUE must admit a peak twin of the same bucket; the old one would reject it.
	if _, err := conn.Exec(
		`INSERT INTO token_usage (user_email, date, step, model, source, report_id, peak, prompt_tokens)
		 VALUES (?, date('now'), 'Analyze', 'deepseek-v4-flash', 'slack', 0, 1, 42)`, email); err != nil {
		t.Fatalf("peak twin rejected by UNIQUE constraint: %v", err)
	}

	var rowCount int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM token_usage WHERE user_email = ?`, email).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("row count = %d, want 2 (off-peak + peak)", rowCount)
	}
}

// TestAddTokenUsageStampsCurrentWindow checks the record-time classification reaches the DB:
// whatever window the flush lands in, the persisted flag matches isPeakWindow for that instant.
func TestAddTokenUsageStampsCurrentWindow(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("setup test DB: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	email := testutil.RandomEmail("peakstamp")

	want := isPeakWindow(time.Now())
	if err := AddTokenUsage(email, "Analyze", "deepseek-v4-flash", "slack", 0, 100, 20, 5, 10); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}
	if err := FlushTokenUsage(ctx); err != nil {
		t.Fatalf("FlushTokenUsage: %v", err)
	}

	var got int
	if err := GetDB().QueryRow(`SELECT peak FROM token_usage WHERE user_email = ?`, email).Scan(&got); err != nil {
		t.Fatalf("read peak flag: %v", err)
	}
	if (got == 1) != want {
		t.Errorf("persisted peak = %d, want %v for the current window", got, want)
	}
}
