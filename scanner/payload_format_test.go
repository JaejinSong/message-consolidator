package scanner

import (
	"context"
	"testing"
	"time"

	"message-consolidator/store"
	"message-consolidator/types"
)

// Why: pins the exact byte-for-byte prompt payload lines the three chat channel
// builders emit today, so the payload_format.go extraction cannot silently change
// what the AI actually sees.

func TestBuildWAPayload_Golden(t *testing.T) {
	initTestDB(t)
	ts := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	user := store.User{Name: "Me", Email: "wa-golden@example.com"}

	cases := []struct {
		name string
		msgs []types.RawMessage
		want string
	}{
		{
			name: "plain message",
			msgs: []types.RawMessage{{ID: "m1", Sender: "+1555", Text: "hi", Timestamp: ts}},
			want: "[ID:m1][ts:2026-07-22T09:30] +1555: hi\n",
		},
		{
			name: "reply-to tag",
			msgs: []types.RawMessage{{ID: "m2", Sender: "+1555", RepliedToUser: "Bob", Text: "sure", Timestamp: ts}},
			want: "[ID:m2][ts:2026-07-22T09:30] [Tags: Reply-To: Bob] +1555: sure\n",
		},
		{
			name: "mentions tag (unresolved)",
			msgs: []types.RawMessage{{ID: "m3", Sender: "+1555", MentionedIDs: []string{"x", "y"}, Text: "hey", Timestamp: ts}},
			want: "[ID:m3][ts:2026-07-22T09:30] [Tags: Mentions: 2] +1555: hey\n",
		},
		{
			name: "attachments/files",
			msgs: []types.RawMessage{{ID: "m4", Sender: "+1555", AttachmentNames: []string{"a.png", "b.pdf"}, Text: "see attached", Timestamp: ts}},
			want: "[ID:m4][ts:2026-07-22T09:30] [Files: a.png, b.pdf] +1555: see attached\n",
		},
		{
			name: "forwarded",
			msgs: []types.RawMessage{{ID: "m5", Sender: "+1555", IsForwarded: true, Text: "fwd", Timestamp: ts}},
			want: "[ID:m5][ts:2026-07-22T09:30] [Tags: Forwarded] +1555: fwd\n",
		},
		{
			name: "from-me",
			msgs: []types.RawMessage{{ID: "m6", Sender: "+1555", IsFromMe: true, Text: "self", Timestamp: ts}},
			want: "[ID:m6][ts:2026-07-22T09:30] Me: self\n",
		},
		{
			name: "multiple messages",
			msgs: []types.RawMessage{
				{ID: "a", Sender: "+1", Text: "x", Timestamp: ts},
				{ID: "b", Sender: "+2", Text: "y", Timestamp: ts},
			},
			want: "[ID:a][ts:2026-07-22T09:30] +1: x\n[ID:b][ts:2026-07-22T09:30] +2: y\n",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := buildWAPayload(context.Background(), user, nil, tc.msgs)
			if payload != tc.want {
				t.Errorf("payload = %q, want %q", payload, tc.want)
			}
		})
	}
}

func TestBuildTGPayload_Golden(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	user := store.User{Name: "Me", Email: "tg-golden@example.com"}

	cases := []struct {
		name string
		msgs []types.RawMessage
		want string
	}{
		{
			name: "plain message",
			msgs: []types.RawMessage{{ID: "m1", SenderName: "Alice", Text: "hi", Timestamp: ts}},
			want: "[ID:m1][ts:2026-07-22T09:30] Alice: hi\n",
		},
		{
			name: "reply-to tag",
			msgs: []types.RawMessage{{ID: "m2", SenderName: "Alice", RepliedToUser: "Bob", Text: "sure", Timestamp: ts}},
			want: "[ID:m2][ts:2026-07-22T09:30] [Tags: Reply-To: Bob] Alice: sure\n",
		},
		{
			name: "has attachment",
			msgs: []types.RawMessage{{ID: "m3", SenderName: "Alice", HasAttachment: true, Text: "see attached", Timestamp: ts}},
			want: "[ID:m3][ts:2026-07-22T09:30] [HasAttachment: true] Alice: see attached\n",
		},
		{
			name: "forwarded",
			msgs: []types.RawMessage{{ID: "m4", SenderName: "Alice", IsForwarded: true, Text: "fwd", Timestamp: ts}},
			want: "[ID:m4][ts:2026-07-22T09:30] [Tags: Forwarded] Alice: fwd\n",
		},
		{
			name: "from-me",
			msgs: []types.RawMessage{{ID: "m5", SenderName: "Alice", IsFromMe: true, Text: "self", Timestamp: ts}},
			want: "[ID:m5][ts:2026-07-22T09:30] Me: self\n",
		},
		{
			name: "multiple messages",
			msgs: []types.RawMessage{
				{ID: "a", SenderName: "Ann", Text: "first", Timestamp: ts},
				{ID: "b", SenderName: "Bob", Text: "second", Timestamp: ts},
			},
			want: "[ID:a][ts:2026-07-22T09:30] Ann: first\n[ID:b][ts:2026-07-22T09:30] Bob: second\n",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, _ := buildTGPayload(user, tc.msgs)
			if payload != tc.want {
				t.Errorf("payload = %q, want %q", payload, tc.want)
			}
		})
	}
}

func TestBuildSlackAnalysisPayload_Golden(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC)
	resolver := fakeSlackResolver{names: map[string]string{"U123": "Bob"}}

	cases := []struct {
		name string
		msgs []types.RawMessage
		want string
	}{
		{
			name: "plain message",
			msgs: []types.RawMessage{{ID: "1.1", SenderName: "Alice", Text: "hi", Timestamp: ts}},
			want: "[ID:1.1][ts:2026-07-22T09:30] Alice: hi\n",
		},
		{
			name: "mentions resolved inline",
			msgs: []types.RawMessage{{ID: "1.2", SenderName: "Alice", Text: "<@U123> please check", Timestamp: ts}},
			want: "[ID:1.2][ts:2026-07-22T09:30] Alice: @Bob please check\n",
		},
		{
			name: "attachments/files",
			msgs: []types.RawMessage{{ID: "1.3", SenderName: "Alice", AttachmentNames: []string{"a.png"}, Text: "see attached", Timestamp: ts}},
			want: "[ID:1.3][ts:2026-07-22T09:30] [Files: a.png] Alice: see attached\n",
		},
		{
			name: "forwarded",
			msgs: []types.RawMessage{{ID: "1.4", SenderName: "Alice", IsForwarded: true, Text: "fwd", Timestamp: ts}},
			want: "[ID:1.4][ts:2026-07-22T09:30] [Tags: Forwarded] Alice: fwd\n",
		},
		{
			name: "sender fallback when SenderName empty",
			msgs: []types.RawMessage{{ID: "1.5", Sender: "U999", Text: "no display name", Timestamp: ts}},
			want: "[ID:1.5][ts:2026-07-22T09:30] U999: no display name\n",
		},
		{
			name: "multiple messages",
			msgs: []types.RawMessage{
				{ID: "a.1", SenderName: "Ann", Text: "first", Timestamp: ts},
				{ID: "a.2", SenderName: "Bob", Text: "second", Timestamp: ts},
			},
			want: "[ID:a.1][ts:2026-07-22T09:30] Ann: first\n[ID:a.2][ts:2026-07-22T09:30] Bob: second\n",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, _ := buildSlackAnalysisPayload(context.Background(), tc.msgs, resolver)
			if payload != tc.want {
				t.Errorf("payload = %q, want %q", payload, tc.want)
			}
		})
	}
}
