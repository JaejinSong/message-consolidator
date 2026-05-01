package scanner

import (
	"testing"
	"time"
)

func TestEnrichSlackMessage(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1700000000, 0)

	cases := []struct {
		name         string
		userID       string
		userName     string
		channelID    string
		threadTS     string
		msg          string
		wantThreadID string
	}{
		{
			name:         "with threadTS uses thread",
			userID:       "U1",
			userName:     "Alice",
			channelID:    "C100",
			threadTS:     "T999",
			msg:          "hi",
			wantThreadID: "slack_thread_T999",
		},
		{
			name:         "empty threadTS falls back to chan",
			userID:       "U2",
			userName:     "Bob",
			channelID:    "C200",
			threadTS:     "",
			msg:          "yo",
			wantThreadID: "slack_thread_C200",
		},
		{
			name:         "both empty falls back to empty",
			userID:       "U3",
			userName:     "Carol",
			channelID:    "",
			threadTS:     "",
			msg:          "x",
			wantThreadID: "slack_thread_",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			enriched, err := EnrichSlackMessage(tc.userID, tc.userName, tc.channelID, tc.threadTS, tc.msg, ts)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if enriched.RawContent != tc.msg {
				t.Errorf("RawContent = %q; want %q", enriched.RawContent, tc.msg)
			}
			if enriched.SourceChannel != "slack" {
				t.Errorf("SourceChannel = %q; want %q", enriched.SourceChannel, "slack")
			}
			if enriched.SenderID != 0 {
				t.Errorf("SenderID = %d; want 0", enriched.SenderID)
			}
			if enriched.SenderName != tc.userName {
				t.Errorf("SenderName = %q; want %q", enriched.SenderName, tc.userName)
			}
			if enriched.VirtualThreadID != tc.wantThreadID {
				t.Errorf("VirtualThreadID = %q; want %q", enriched.VirtualThreadID, tc.wantThreadID)
			}
			if !enriched.Timestamp.Equal(ts) {
				t.Errorf("Timestamp = %v; want %v", enriched.Timestamp, ts)
			}
		})
	}
}
