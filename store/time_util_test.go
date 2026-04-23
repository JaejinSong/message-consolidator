package store

import (
	"testing"
	"time"
)

func TestGetSQLiteOffset(t *testing.T) {
	tests := []struct {
		tz       string
		contains string // Sign and basic digits as DST might change exact offset
	}{
		{"Asia/Seoul", "+09"},
		{"UTC", "+00"},
		{"", "+00"},
		{"Invalid/Zone", "+00"},
		{"America/New_York", "-0"}, // Covers -04 or -05
		{"Europe/London", "+0"},    // Covers +00 or +01
	}

	for _, tt := range tests {
		got := GetSQLiteOffset(tt.tz)
		if len(got) != 6 {
			t.Errorf("GetSQLiteOffset(%q) = %q; want length 6 (e.g., +00:00)", tt.tz, got)
		}
		//Why: Performs a basic format check for [+-]HH:MM to ensure SQLite compatibility.
		if got[3] != ':' {
			t.Errorf("GetSQLiteOffset(%q) = %q; missing colon at index 3", tt.tz, got)
		}
	}
}

func TestGetWorkingDaysAgo(t *testing.T) {
	//Why: Uses 2026-03-24 (Tuesday) as a fixed reference point for deterministic weekend-skipping tests.
	now := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)

	//Why: Calculating 1 working day ago from Tuesday should land exactly on Monday.
	got1 := GetWorkingDaysAgo(1, now)
	if got1.Day() != 23 {
		t.Errorf("GetWorkingDaysAgo(1) = %v; want day 23", got1)
	}

	//Why: Calculating 3 working days ago from Tuesday should skip Sunday and Saturday to land on the previous Thursday.
	got3 := GetWorkingDaysAgo(3, now)
	if got3.Day() != 19 {
		t.Errorf("GetWorkingDaysAgo(3) = %v; want day 19", got3)
	}
}

func TestGetLocalThreshold(t *testing.T) {
	//Why: Verifies that the threshold generation logic is robust against different timezones and doesn't produce panic-inducing malformed strings.
	res := GetLocalThreshold("Asia/Seoul", 3)
	_, err := time.Parse(time.RFC3339, res)
	if err != nil {
		t.Errorf("GetLocalThreshold produced invalid RFC3339: %v", err)
	}
}

