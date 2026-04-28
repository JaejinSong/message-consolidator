package services

import (
	"database/sql"
	"message-consolidator/store"
	"testing"
	"time"
)

// makeKSTTime creates a time.Time in Asia/Seoul for test helpers.
func makeKSTTime(year, month, day int) time.Time {
	loc, _ := time.LoadLocation("Asia/Seoul")
	return time.Date(year, time.Month(month), day, 18, 0, 0, 0, loc)
}

func nullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

func TestFormatDigest_Empty(t *testing.T) {
	snap := store.DigestSnapshot{
		Received:      nil,
		Assigned:      nil,
		ReceivedTotal: 0,
		AssignedTotal: 0,
	}
	now := makeKSTTime(2026, 4, 28) // Tuesday
	text := formatDigest(snap, now)

	if text == "" {
		t.Fatal("expected non-empty output")
	}
	assertContains(t, text, "2026-04-28")
	assertContains(t, text, "(화)")
	assertContains(t, text, "받은 업무* — 0건")
	assertContains(t, text, "맡긴 업무* — 0건")
	assertContains(t, text, "(없음)")
}

func TestFormatDigest_WithItems(t *testing.T) {
	now := makeKSTTime(2026, 4, 28)
	snap := store.DigestSnapshot{
		Received: []store.DigestTask{
			{
				ID:          1,
				Task:        "API 응답 포맷 정리",
				Source:      "Slack",
				Room:        "dev",
				CreatedAt:   now,
				Deadline:    nullStr("2026-04-30"),
				Counterpart: "김PM",
			},
			{
				ID:          2,
				Task:        "인보이스 검토 요청",
				Source:      "Gmail",
				Room:        "",
				CreatedAt:   now,
				Deadline:    nullStr(""),
				Counterpart: "정OO",
			},
		},
		ReceivedTotal: 2,
		Assigned: []store.DigestTask{
			{
				ID:          3,
				Task:        "코드 리뷰",
				Source:      "Slack",
				Room:        "backend",
				CreatedAt:   now,
				Deadline:    nullStr(""),
				Counterpart: "이개발",
			},
		},
		AssignedTotal: 1,
	}

	text := formatDigest(snap, now)

	assertContains(t, text, "받은 업무* — 2건")
	assertContains(t, text, "[Slack/dev] API 응답 포맷 정리")
	assertContains(t, text, "← 김PM")
	assertContains(t, text, "~04-30")
	assertContains(t, text, "[Gmail] 인보이스 검토 요청")
	assertContains(t, text, "← 정OO")
	assertContains(t, text, "맡긴 업무* — 1건")
	assertContains(t, text, "[Slack/backend] 코드 리뷰")
	assertContains(t, text, "→ 이개발")
}

func TestFormatDigest_LimitTruncation(t *testing.T) {
	now := makeKSTTime(2026, 4, 28)

	// 2 items shown but total is 7 → "외 5건"
	snap := store.DigestSnapshot{
		Received: []store.DigestTask{
			{ID: 1, Task: "task1", Source: "Slack", Room: "a"},
			{ID: 2, Task: "task2", Source: "Slack", Room: "b"},
		},
		ReceivedTotal: 7,
		AssignedTotal: 0,
	}

	text := formatDigest(snap, now)
	assertContains(t, text, "외 5건")
}

func TestFormatDigest_TaskTrim80Runes(t *testing.T) {
	// Build a 100-rune Korean string (each 가 is one rune).
	longTask := ""
	for i := 0; i < 100; i++ {
		longTask += "가"
	}

	now := makeKSTTime(2026, 4, 28)
	snap := store.DigestSnapshot{
		Received: []store.DigestTask{
			{ID: 1, Task: longTask, Source: "Slack", Room: "ch"},
		},
		ReceivedTotal: 1,
		AssignedTotal: 0,
	}

	text := formatDigest(snap, now)

	// Verify truncation: the rendered task should be ≤ 80 runes + "…"
	// Find the line that starts with "• "
	for _, line := range splitLines(text) {
		runes := []rune(line)
		if len(runes) > 0 && runes[0] == '•' {
			// Extract task portion after "[Slack/ch] "
			if len(runes) > 100 {
				t.Errorf("task line exceeds expected length: %d runes", len(runes))
			}
		}
	}
	assertContains(t, text, "…")
}

func TestFormatDigest_WeekdayKorean(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Seoul")

	cases := []struct {
		date    time.Time
		wantDay string
	}{
		{time.Date(2026, 4, 28, 18, 0, 0, 0, loc), "화"},
		{time.Date(2026, 5, 1, 18, 0, 0, 0, loc), "금"},
	}

	for _, tc := range cases {
		snap := store.DigestSnapshot{}
		text := formatDigest(snap, tc.date)
		want := "(" + tc.wantDay + ")"
		assertContains(t, text, want)
	}
}

// assertContains is a helper that fails the test if s does not contain sub.
func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !contains(s, sub) {
		t.Errorf("expected output to contain %q\ngot:\n%s", sub, s)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
