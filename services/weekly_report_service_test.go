package services

import (
	"testing"
	"time"
)

func TestComputeWeekWindow(t *testing.T) {
	cases := []struct {
		name      string
		now       string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "friday_18h",
			now:       "2026-05-01 18:00",
			wantStart: "2026-04-25",
			wantEnd:   "2026-05-01",
		},
		{
			name:      "next_friday_18h",
			now:       "2026-05-08 18:00",
			wantStart: "2026-05-02",
			wantEnd:   "2026-05-08",
		},
		{
			name:      "thursday_18h_pure_fn",
			now:       "2026-04-30 18:00",
			wantStart: "2026-04-24",
			wantEnd:   "2026-04-30",
		},
	}

	loc := time.UTC
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.ParseInLocation("2006-01-02 15:04", tc.now, loc)
			if err != nil {
				t.Fatalf("parse time: %v", err)
			}
			gotStart, gotEnd := computeWeekWindow(now)
			if gotStart != tc.wantStart {
				t.Errorf("start: want %q got %q", tc.wantStart, gotStart)
			}
			if gotEnd != tc.wantEnd {
				t.Errorf("end: want %q got %q", tc.wantEnd, gotEnd)
			}
		})
	}
}

func TestFormatWeeklyDMText(t *testing.T) {
	start := "2026-04-25"
	end := "2026-05-01"
	url := "https://notion.so/page-abc"

	got := formatWeeklyDMText(start, end, url)
	for _, want := range []string{start, end, url} {
		if !containsString(got, want) {
			t.Errorf("result %q does not contain %q", got, want)
		}
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
