package services

import (
	"testing"
	"time"
)

func TestComputeDailyWindow(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")
	cases := []struct {
		name      string
		now       time.Time
		wantStart string
		wantEnd   string
	}{
		{"tuesday", time.Date(2026, 4, 28, 18, 0, 0, 0, loc), "2026-04-28", "2026-04-28"},
		{"wednesday", time.Date(2026, 4, 29, 18, 0, 0, 0, loc), "2026-04-29", "2026-04-29"},
		{"thursday", time.Date(2026, 4, 30, 18, 0, 0, 0, loc), "2026-04-30", "2026-04-30"},
		{"friday", time.Date(2026, 5, 1, 18, 0, 0, 0, loc), "2026-05-01", "2026-05-01"},
		{"monday_includes_weekend", time.Date(2026, 5, 4, 18, 0, 0, 0, loc), "2026-05-02", "2026-05-04"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := computeDailyWindow(tc.now)
			if gotStart != tc.wantStart {
				t.Errorf("start: want %q got %q", tc.wantStart, gotStart)
			}
			if gotEnd != tc.wantEnd {
				t.Errorf("end: want %q got %q", tc.wantEnd, gotEnd)
			}
		})
	}
}

func TestFormatDailyDMText(t *testing.T) {
	t.Run("same_day", func(t *testing.T) {
		got := formatDailyDMText("2026-04-28", "2026-04-28", "summary body")
		for _, want := range []string{"Daily Report", "2026-04-28", "summary body"} {
			if !containsString(got, want) {
				t.Errorf("result %q missing %q", got, want)
			}
		}
		if containsString(got, "~") {
			t.Errorf("same-day form should not contain '~': %q", got)
		}
	})
	t.Run("monday_range", func(t *testing.T) {
		got := formatDailyDMText("2026-05-02", "2026-05-04", "weekend body")
		for _, want := range []string{"2026-05-02", "2026-05-04", "~", "weekend body"} {
			if !containsString(got, want) {
				t.Errorf("result %q missing %q", got, want)
			}
		}
	})
}
