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
		// Basic check for format [+-]HH:MM
		if got[3] != ':' {
			t.Errorf("GetSQLiteOffset(%q) = %q; missing colon at index 3", tt.tz, got)
		}
	}
}

func TestGetWorkingDaysAgo(t *testing.T) {
	// 2026-03-24 (Tue)
	now := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)

	// 1 working day ago -> 2026-03-23 (Mon)
	got1 := GetWorkingDaysAgo(1, now)
	if got1.Day() != 23 {
		t.Errorf("GetWorkingDaysAgo(1) = %v; want day 23", got1)
	}

	// 3 working days ago -> 2026-03-19 (Thu)
	// (24-Tue, 23-Mon, 20-Fri, 19-Thu)
	got3 := GetWorkingDaysAgo(3, now)
	if got3.Day() != 19 {
		t.Errorf("GetWorkingDaysAgo(3) = %v; want day 19", got3)
	}
}

func TestGetLocalThreshold(t *testing.T) {
	// Should not panic and return a valid RFC3339 string
	res := GetLocalThreshold("Asia/Seoul", 3)
	_, err := time.Parse(time.RFC3339, res)
	if err != nil {
		t.Errorf("GetLocalThreshold produced invalid RFC3339: %v", err)
	}
}

func TestDBTime_Scan(t *testing.T) {
	now := time.Now().Round(time.Second) // 초 단위 절사
	nowStr := now.Format("2006-01-02 15:04:05")

	tests := []struct {
		name      string
		input     interface{}
		wantValid bool
		wantTime  time.Time
		wantErr   bool
	}{
		{
			name:      "Scan time.Time directly",
			input:     now,
			wantValid: true,
			wantTime:  now,
			wantErr:   false,
		},
		{
			name:      "Scan standard DB string",
			input:     nowStr,
			wantValid: true,
			wantTime:  now,
			wantErr:   false,
		},
		{
			name:      "Scan RFC3339 string",
			input:     "2026-03-24T07:28:04Z",
			wantValid: true,
			wantTime:  time.Date(2026, 3, 24, 7, 28, 4, 0, time.UTC),
			wantErr:   false,
		},
		{
			name:      "Scan []byte",
			input:     []byte("2026-03-24 07:28:04"),
			wantValid: true,
			wantTime:  time.Date(2026, 3, 24, 7, 28, 4, 0, time.UTC),
			wantErr:   false,
		},
		{
			name:      "Scan nil",
			input:     nil,
			wantValid: false,
			wantTime:  time.Time{},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d DBTime
			err := d.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if d.Valid != tt.wantValid {
				t.Errorf("Scan() Valid = %v, want %v", d.Valid, tt.wantValid)
			}
			if !d.Time.Equal(tt.wantTime) {
				t.Errorf("Scan() Time = %v, want %v", d.Time, tt.wantTime)
			}
		})
	}
}
