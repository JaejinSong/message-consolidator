package scanner

import (
	"strconv"
	"testing"
	"time"
)

func TestSlackScanWindow(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) string {
		return formatSlackTS(now.Add(-d))
	}

	cases := []struct {
		name  string
		minTS string
		want  time.Time
	}{
		{
			// Why: no cursor means a fresh channel; pulling a week of backlog on first
			// sight would spend AI budget on history nobody is waiting for.
			name:  "no cursor falls back to the default lookback",
			minTS: "",
			want:  now.Add(-24 * time.Hour),
		},
		{
			// Why: the cursor only says what was seen, not what was analyzed — keep the
			// 24h floor so a message that failed mid-run gets another pass.
			name:  "recent cursor still scans the full default window",
			minTS: ts(2 * time.Hour),
			want:  now.Add(-24 * time.Hour),
		},
		{
			// Why: this is the gap the old code lost — the comment promised minTS as a
			// lower bound while `since` cut at 24h, so a longer outage skipped the
			// middle and the cursor jumped past it.
			name:  "cursor older than a day widens the window to the cursor",
			minTS: ts(72 * time.Hour),
			want:  now.Add(-72 * time.Hour),
		},
		{
			// Why: an unbounded catch-up would blow the 60s scan timeout and re-analyze
			// weeks of history at once.
			name:  "very old cursor is capped at the catch-up limit",
			minTS: ts(45 * 24 * time.Hour),
			want:  now.Add(-slackCatchUpCap),
		},
		{
			// Why: regression for the 2026-09-17 bot-removal outage — cursor 10 days
			// old must widen to the cursor itself, not the old 7-day cap.
			name:  "cursor 10 days old widens to the cursor",
			minTS: ts(10 * 24 * time.Hour),
			want:  now.Add(-10 * 24 * time.Hour),
		},
		{
			// Why: cursor older than the new 29-day cap floors at now-29d.
			name:  "cursor 40 days old floors at the 29-day cap",
			minTS: ts(40 * 24 * time.Hour),
			want:  now.Add(-slackCatchUpCap),
		},
		{
			name:  "unparsable cursor falls back to the default lookback",
			minTS: "not-a-timestamp",
			want:  now.Add(-24 * time.Hour),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slackScanWindow(tc.minTS, now)
			if !got.Equal(tc.want) {
				t.Errorf("slackScanWindow(%q) = %v, want %v", tc.minTS, got, tc.want)
			}
		})
	}
}

func formatSlackTS(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + ".000000"
}
