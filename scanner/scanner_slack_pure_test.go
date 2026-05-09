package scanner

import (
	"context"
	"strings"
	"testing"

	"message-consolidator/store"
	"message-consolidator/types"
)

// TestTrackedThreadSet verifies set-building from a thread slice.
func TestTrackedThreadSet(t *testing.T) {
	t.Parallel()
	threads := []store.SlackThreadMeta{
		{ThreadTS: "1.0"},
		{ThreadTS: "2.0"},
		{ThreadTS: "1.0"}, // duplicate
	}
	got := trackedThreadSet(threads)
	if len(got) != 2 {
		t.Errorf("trackedThreadSet len = %d, want 2", len(got))
	}
	for _, ts := range []string{"1.0", "2.0"} {
		if _, ok := got[ts]; !ok {
			t.Errorf("trackedThreadSet missing %q", ts)
		}
	}
}

func TestTrackedThreadSet_Empty(t *testing.T) {
	t.Parallel()
	got := trackedThreadSet(nil)
	if len(got) != 0 {
		t.Errorf("trackedThreadSet(nil) len = %d, want 0", len(got))
	}
}

// TestUpdateChannelCursor covers the first-write and max-update paths.
func TestUpdateChannelCursor(t *testing.T) {
	t.Parallel()
	// Why: Slack timestamp format is 10-digit unix seconds + fractional part.
	// String comparison is correct here: same-length timestamps compare correctly lexicographically.
	tests := []struct {
		name      string
		initial   map[string]map[string]string
		email     string
		channelID string
		msgID     string
		want      string
	}{
		{
			name:      "new email initialises inner map",
			initial:   map[string]map[string]string{},
			email:     "u@x",
			channelID: "C1",
			msgID:     "1700000100.000000",
			want:      "1700000100.000000",
		},
		{
			name:      "larger ID overwrites existing cursor",
			initial:   map[string]map[string]string{"u@x": {"C1": "1700000100.000000"}},
			email:     "u@x",
			channelID: "C1",
			msgID:     "1700000200.000000",
			want:      "1700000200.000000",
		},
		{
			name:      "smaller ID does not overwrite existing cursor",
			initial:   map[string]map[string]string{"u@x": {"C1": "1700000200.000000"}},
			email:     "u@x",
			channelID: "C1",
			msgID:     "1700000100.000000",
			want:      "1700000200.000000",
		},
		{
			name:      "equal ID does not change cursor",
			initial:   map[string]map[string]string{"u@x": {"C1": "1700000100.000000"}},
			email:     "u@x",
			channelID: "C1",
			msgID:     "1700000100.000000",
			want:      "1700000100.000000",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			newTS := tt.initial
			updateChannelCursor(newTS, tt.email, tt.channelID, tt.msgID)
			if got := newTS[tt.email][tt.channelID]; got != tt.want {
				t.Errorf("updateChannelCursor result = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildSlackMetadataString covers all tag and reaction combinations.
func TestBuildSlackMetadataString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		m           types.RawMessage
		wantContain []string
		wantAbsent  []string
		wantEqual   *string
	}{
		{
			name:      "empty message returns empty string",
			m:         types.RawMessage{},
			wantEqual: strPtr(""),
		},
		{
			name:        "pinned message has Pinned tag",
			m:           types.RawMessage{IsPinned: true},
			wantContain: []string{"[Tags: Pinned]"},
		},
		{
			name:        "important message has Important tag",
			m:           types.RawMessage{IsImportant: true},
			wantContain: []string{"[Tags: Important]"},
		},
		{
			name:        "forwarded message has Forwarded tag",
			m:           types.RawMessage{IsForwarded: true},
			wantContain: []string{"[Tags: Forwarded]"},
		},
		{
			name:        "pinned + important combines tags",
			m:           types.RawMessage{IsPinned: true, IsImportant: true},
			wantContain: []string{"Pinned", "Important"},
		},
		{
			name:        "reactions are listed",
			m:           types.RawMessage{Reactions: []string{"+1", "tada"}},
			wantContain: []string{"[Reactions: +1, tada]"},
			wantAbsent:  []string{"Tags"},
		},
		{
			name:        "attachments are listed",
			m:           types.RawMessage{AttachmentNames: []string{"file.pdf", "img.png"}},
			wantContain: []string{"[Files: file.pdf, img.png]"},
		},
		{
			name:        "all fields populated",
			m:           types.RawMessage{IsPinned: true, Reactions: []string{"ok"}, AttachmentNames: []string{"doc.pdf"}},
			wantContain: []string{"[Tags: Pinned]", "[Reactions: ok]", "[Files: doc.pdf]"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildSlackMetadataString(tt.m)
			if tt.wantEqual != nil {
				if got != *tt.wantEqual {
					t.Errorf("buildSlackMetadataString() = %q, want %q", got, *tt.wantEqual)
				}
				return
			}
			for _, sub := range tt.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("buildSlackMetadataString() = %q, want to contain %q", got, sub)
				}
			}
			for _, sub := range tt.wantAbsent {
				if strings.Contains(got, sub) {
					t.Errorf("buildSlackMetadataString() = %q, want NOT to contain %q", got, sub)
				}
			}
		})
	}
}

// TestResolveSlackMentions_AdditionalCases extends the existing tests with
// edge cases not covered by the scanner_test.go suite.
func TestResolveSlackMentions_AdditionalCases(t *testing.T) {
	t.Parallel()
	sc := mockSlackResolver{users: map[string]string{"U1": "Alice"}}

	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "no mentions returns text unchanged",
			text: "hello world",
			want: "hello world",
		},
		{
			name: "empty text returns empty",
			text: "",
			want: "",
		},
		{
			name: "unknown user ID left as-is",
			text: "<@UUNKNOWN> hi",
			want: "<@UUNKNOWN> hi",
		},
		{
			name: "known user is resolved",
			text: "<@U1> hi",
			want: "@Alice hi",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveSlackMentions(context.Background(), tt.text, sc); got != tt.want {
				t.Errorf("resolveSlackMentions() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestUpdateSlackCursors_EmptyMap verifies no panic on empty input.
func TestUpdateSlackCursors_EmptyMap(t *testing.T) {
	// Should not panic.
	updateSlackCursors(map[string]map[string]string{})
}

// TestUpdateSlackCursors_WithEntries calls the real store path (in-memory).
func TestUpdateSlackCursors_WithEntries(t *testing.T) {
	email := "cursor-test@example.com"
	chanID := "C-CURSOR-1"
	_ = store.UpdateLastScan(email, "slack", chanID, "old.000")

	newTS := map[string]map[string]string{
		email: {chanID: "new.000"},
	}
	updateSlackCursors(newTS)

	got := store.GetLastScan(email, "slack", chanID)
	if got != "new.000" {
		t.Errorf("GetLastScan = %q, want %q", got, "new.000")
	}
}
