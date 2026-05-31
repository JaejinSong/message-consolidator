package services

import (
	"testing"
	"time"
)

func TestParseDeadline(t *testing.T) {
	// ref = Wednesday 2026-06-03
	ref := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		raw      string
		wantISO  string
		wantInf  bool
	}{
		// empty / garbage
		{"empty", "", "", false},
		{"whitespace", "  ", "", false},
		{"garbage", "soon", "", false},
		{"ASAP", "ASAP", "", false},
		{"조만간", "조만간", "", false},

		// ISO passthrough
		{"iso date", "2026-06-10", "2026-06-10", false},
		{"iso date past", "2025-01-01", "2025-01-01", false},

		// today / eod
		{"today", "today", "2026-06-03", true},
		{"오늘", "오늘", "2026-06-03", true},
		{"eod", "eod", "2026-06-03", true},
		{"end of day", "end of day", "2026-06-03", true},

		// tomorrow
		{"tomorrow", "tomorrow", "2026-06-04", true},
		{"내일", "내일", "2026-06-04", true},

		// this week → nearest Friday from Wed 2026-06-03 = 2026-06-05
		{"this week", "this week", "2026-06-05", true},
		{"이번주", "이번주", "2026-06-05", true},

		// weekday — next occurrence from ref (Wed 2026-06-03)
		{"friday bare", "friday", "2026-06-05", true},    // Fri = 2 days away
		{"thursday bare", "thursday", "2026-06-04", true}, // Thu = 1 day away
		{"monday bare", "monday", "2026-06-08", true},    // Mon = 5 days away
		{"금요일", "금요일", "2026-06-05", true},

		// "by" prefix — same as bare
		{"by friday", "by friday", "2026-06-05", true},
		{"by monday", "by monday", "2026-06-08", true},

		// "next" prefix — same as bare (simplest consistent behaviour)
		{"next friday", "next friday", "2026-06-05", true},
		{"next monday", "next monday", "2026-06-08", true},

		// ref is Friday — same-day match returns today
		{"friday when ref is fri", "friday", "2026-06-05", true},
	}

	// Override ref for the "friday when ref is fri" case
	refFri := time.Date(2026, 6, 5, 9, 0, 0, 0, time.UTC)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ref
			if tc.name == "friday when ref is fri" {
				r = refFri
			}
			gotISO, gotInf := ParseDeadline(tc.raw, r)
			if gotISO != tc.wantISO {
				t.Errorf("ParseDeadline(%q) iso = %q, want %q", tc.raw, gotISO, tc.wantISO)
			}
			if gotInf != tc.wantInf {
				t.Errorf("ParseDeadline(%q) inferred = %v, want %v", tc.raw, gotInf, tc.wantInf)
			}
		})
	}
}
