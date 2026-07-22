package scanner

import (
	"context"
	"testing"
	"time"

	"message-consolidator/types"
)

// fakeSlackResolver resolves known user IDs without touching the Slack API.
type fakeSlackResolver struct{ names map[string]string }

func (f fakeSlackResolver) GetUserName(_ context.Context, id string) string {
	if n, ok := f.names[id]; ok {
		return n
	}
	return id
}

// TestSlackPayloadCharacterization pins the analysis payload format (ID/ts tags,
// metadata tags, mention resolution, sender fallback) across the adapter migration.
func TestSlackPayloadCharacterization(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	msgs := []types.RawMessage{
		{
			ID: "1752800000.000100", Sender: "Alice", SenderName: "Alice",
			Text: "<@U123> 보고서 검토 부탁해요", Timestamp: ts,
			IsPinned: true, Reactions: []string{"+1"},
		},
		{
			ID: "1752800060.000200", Sender: "U999", SenderName: "",
			Text: "확인했습니다", Timestamp: time.Time{},
		},
	}
	resolver := fakeSlackResolver{names: map[string]string{"U123": "Bob"}}

	payload, msgMap := buildSlackAnalysisPayload(context.Background(), msgs, resolver)

	want := "[ID:1752800000.000100][ts:2026-07-22T09:30] [Tags: Pinned] [Reactions: +1] Alice: @Bob 보고서 검토 부탁해요\n" +
		"[ID:1752800060.000200] U999: 확인했습니다\n"
	if payload != want {
		t.Errorf("payload mismatch\n got: %q\nwant: %q", payload, want)
	}
	if len(msgMap) != 2 {
		t.Fatalf("expected 2 mapped msgs, got %d", len(msgMap))
	}
	if _, ok := msgMap["1752800000.000100"]; !ok {
		t.Error("msgMap must be keyed by message ID")
	}
}

// TestSlackThreadAnchorCharacterization pins the thread anchor rule: replies
// anchor on the parent thread ts, root messages on their own ID.
func TestSlackThreadAnchorCharacterization(t *testing.T) {
	t.Parallel()
	if got := slackThreadTS(types.RawMessage{ID: "root.1", ReplyToID: ""}); got != "root.1" {
		t.Errorf("root anchor = %q, want %q", got, "root.1")
	}
	if got := slackThreadTS(types.RawMessage{ID: "child.2", ReplyToID: "root.1"}); got != "root.1" {
		t.Errorf("reply anchor = %q, want %q", got, "root.1")
	}
}
