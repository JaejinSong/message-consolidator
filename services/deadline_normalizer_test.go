package services

import (
	"testing"
	"time"
)

func TestParseDeadline(t *testing.T) {
	// ref = Wednesday 2026-06-03
	ref := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		raw     string
		wantISO string
		wantInf bool
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
		{"friday bare", "friday", "2026-06-05", true},     // Fri = 2 days away
		{"thursday bare", "thursday", "2026-06-04", true}, // Thu = 1 day away
		{"monday bare", "monday", "2026-06-08", true},     // Mon = 5 days away
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

// TestParseDeadlineExtendedVocabulary covers the surface forms that live traffic
// carries but the original vocabulary rejected: Korean particles, explicit
// next-week qualifiers, Indonesian, period boundaries and absolute dates.
func TestParseDeadlineExtendedVocabulary(t *testing.T) {
	// refWed = Wednesday 2026-06-03
	refWed := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	// refSun = Sunday 2026-06-07
	refSun := time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		raw     string
		ref     time.Time
		wantISO string
		wantInf bool
	}{
		// Korean particles / suffixes
		{"화요일까지", "화요일까지", refWed, "2026-06-09", true},
		{"내일까지", "내일까지", refWed, "2026-06-04", true},
		{"오늘 중으로", "오늘 중으로", refWed, "2026-06-03", true},
		{"금요일 안에", "금요일 안에", refWed, "2026-06-05", true},

		// explicit next-week qualifier resolves into the following week
		{"다음주 화요일", "다음주 화요일", refWed, "2026-06-09", true},
		{"다음주화요일", "다음주화요일", refWed, "2026-06-09", true},
		{"다음주 화요일까지", "다음주 화요일까지", refWed, "2026-06-09", true},
		{"다음주 월요일", "다음주 월요일", refWed, "2026-06-08", true},
		{"차주 금요일", "차주 금요일", refWed, "2026-06-12", true},
		{"이번주 금요일", "이번주 금요일", refWed, "2026-06-05", true},

		// next week from a Sunday ref -- previously landed a week late
		{"next week from sunday", "next week", refSun, "2026-06-08", true},
		{"다음주 from sunday", "다음주", refSun, "2026-06-08", true},

		// Indonesian
		{"besok", "besok", refWed, "2026-06-04", true},
		{"hari ini", "hari ini", refWed, "2026-06-03", true},
		{"minggu depan", "minggu depan", refWed, "2026-06-08", true},
		{"minggu ini", "minggu ini", refWed, "2026-06-05", true},
		{"kamis", "kamis", refWed, "2026-06-04", true},
		{"kamis depan", "kamis depan", refWed, "2026-06-11", true},
		{"paling lambat jumat", "paling lambat jumat", refWed, "2026-06-05", true},

		// period boundaries
		{"end of week", "end of week", refWed, "2026-06-05", true},
		{"eow", "eow", refWed, "2026-06-05", true},
		{"end of month", "end of month", refWed, "2026-06-30", true},
		{"월말", "월말", refWed, "2026-06-30", true},
		{"akhir bulan", "akhir bulan", refWed, "2026-06-30", true},
		{"next month", "next month", refWed, "2026-07-01", true},
		{"다음달", "다음달", refWed, "2026-07-01", true},

		// absolute dates
		{"slash month day", "9/12", refWed, "2026-09-12", true},
		{"dotted full date", "2026.09.12", refWed, "2026-09-12", true},
		{"korean month day", "6월 10일", refWed, "2026-06-10", true},
		{"korean month day tight", "6월10일", refWed, "2026-06-10", true},
		{"english month day", "sep 12", refWed, "2026-09-12", true},
		{"english month day full", "September 12", refWed, "2026-09-12", true},
		{"day then month short", "6 sep", refWed, "2026-09-06", true},
		{"day then month ordinal", "04 aug", refWed, "2026-08-04", true},
		{"day then month with dot", "7 sep.", refWed, "2026-09-07", true},
		{"day only ordinal", "the 27th", refWed, "2026-06-27", true},
		{"day only korean", "12일", refWed, "2026-06-12", true},
		{"day only rolls to next month when past", "1일", refWed, "2026-07-01", true},

		// still unparseable -- must not invent a date
		{"vague soon", "asap", refWed, "", false},
		{"vague nanti", "nanti", refWed, "", false},
		{"vague 조만간", "조만간", refWed, "", false},
		{"bare number", "5", refWed, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotISO, gotInf := ParseDeadline(tc.raw, tc.ref)
			if gotISO != tc.wantISO {
				t.Errorf("ParseDeadline(%q) iso = %q, want %q", tc.raw, gotISO, tc.wantISO)
			}
			if gotInf != tc.wantInf {
				t.Errorf("ParseDeadline(%q) inferred = %v, want %v", tc.raw, gotInf, tc.wantInf)
			}
		})
	}
}
