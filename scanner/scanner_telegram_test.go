package scanner

import (
	"strings"
	"testing"
	"time"

	"message-consolidator/store"
	"message-consolidator/types"
)

func TestTelegramAdapter_Source(t *testing.T) {
	t.Parallel()
	if got := (telegramAdapter{}).Source(); got != "telegram" {
		t.Fatalf("Source() = %q, want %q", got, "telegram")
	}
}

func TestTelegramAdapter_LogPrefix(t *testing.T) {
	t.Parallel()
	if got := (telegramAdapter{}).LogPrefix(); got != "TG" {
		t.Fatalf("LogPrefix() = %q, want %q", got, "TG")
	}
}

func TestTelegramAdapter_Is1To1(t *testing.T) {
	t.Parallel()
	cases := []struct {
		roomKey string
		want    bool
	}{
		{"tg_user_123", true},
		{"tg_user_", true},
		{"tg_channel_456", false},
		{"tg_chat_789", false},
		{"", false},
		// Why: HasPrefix is case-sensitive; uppercase prefix must not match.
		{"TG_USER_123", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.roomKey, func(t *testing.T) {
			t.Parallel()
			if got := (telegramAdapter{}).Is1To1(tc.roomKey); got != tc.want {
				t.Errorf("Is1To1(%q) = %v, want %v", tc.roomKey, got, tc.want)
			}
		})
	}
}

func TestBuildTGMetadataString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		msg         types.RawMessage
		wantContain []string
		wantAbsent  []string
		wantEqual   *string
	}{
		{
			name:      "empty message has no metadata",
			msg:       types.RawMessage{},
			wantEqual: strPtr(""),
		},
		{
			name:        "forwarded only",
			msg:         types.RawMessage{IsForwarded: true},
			wantContain: []string{"[Tags: Forwarded]"},
			wantAbsent:  []string{"Reply-To"},
		},
		{
			name:        "reply-to only",
			msg:         types.RawMessage{RepliedToUser: "Hady"},
			wantContain: []string{"[Tags: Reply-To: Hady]"},
			wantAbsent:  []string{"Forwarded"},
		},
		{
			name:        "forwarded + reply-to combined",
			msg:         types.RawMessage{IsForwarded: true, RepliedToUser: "Hady"},
			wantContain: []string{"Forwarded, Reply-To: Hady"},
		},
		{
			name:        "has attachment only",
			msg:         types.RawMessage{HasAttachment: true},
			wantContain: []string{"[HasAttachment: true]"},
			wantAbsent:  []string{"Tags"},
		},
		{
			name:        "forwarded + attachment",
			msg:         types.RawMessage{IsForwarded: true, HasAttachment: true},
			wantContain: []string{"[Tags: Forwarded]", "[HasAttachment: true]"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildTGMetadataString(tc.msg)
			if tc.wantEqual != nil {
				if got != *tc.wantEqual {
					t.Errorf("buildTGMetadataString() = %q, want %q", got, *tc.wantEqual)
				}
				return
			}
			for _, sub := range tc.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("buildTGMetadataString() = %q, want to contain %q", got, sub)
				}
			}
			for _, sub := range tc.wantAbsent {
				if strings.Contains(got, sub) {
					t.Errorf("buildTGMetadataString() = %q, want NOT to contain %q", got, sub)
				}
			}
		})
	}
}

func TestBuildTGPayload(t *testing.T) {
	t.Parallel()
	ts := time.Unix(1700000000, 0)
	cases := []struct {
		name          string
		user          store.User
		msgs          []types.RawMessage
		wantContain   []string
		wantMsgMapLen int
		wantMsgMap    map[string]string // id → expected Text
	}{
		{
			name: "single message uses SenderName",
			user: store.User{Name: "Me", Email: "me@x"},
			msgs: []types.RawMessage{
				{ID: "m1", SenderName: "Alice", Sender: "alice_handle", Text: "hi", Timestamp: ts},
			},
			wantContain:   []string{"[ID:m1]", " Alice: hi"},
			wantMsgMapLen: 1,
			wantMsgMap:    map[string]string{"m1": "hi"},
		},
		{
			name: "SenderName empty falls back to Sender",
			user: store.User{Name: "Me"},
			msgs: []types.RawMessage{
				{ID: "m2", SenderName: "", Sender: "bob_handle", Text: "yo", Timestamp: ts},
			},
			wantContain:   []string{"[ID:m2]", "bob_handle: yo"},
			wantMsgMapLen: 1,
			wantMsgMap:    map[string]string{"m2": "yo"},
		},
		{
			name: "IsFromMe overrides to user.Name",
			user: store.User{Name: "Owner"},
			msgs: []types.RawMessage{
				{ID: "m3", SenderName: "AnyName", Sender: "any", IsFromMe: true, Text: "self", Timestamp: ts},
			},
			// Why: IsFromMe must replace SenderName entirely, not just fall back.
			wantContain:   []string{"[ID:m3]", "Owner: self"},
			wantMsgMapLen: 1,
			wantMsgMap:    map[string]string{"m3": "self"},
		},
		{
			name: "multiple messages preserved in order with msgMap",
			user: store.User{Name: "Me"},
			msgs: []types.RawMessage{
				{ID: "a", SenderName: "Ann", Text: "first", Timestamp: ts},
				{ID: "b", SenderName: "Bob", Text: "second", Timestamp: ts},
			},
			wantContain:   []string{"[ID:a]", "[ID:b]"},
			wantMsgMapLen: 2,
			wantMsgMap:    map[string]string{"a": "first", "b": "second"},
		},
		{
			name: "metadata string is included between ID and sender",
			user: store.User{Name: "Me"},
			msgs: []types.RawMessage{
				{ID: "m4", SenderName: "Sender", IsForwarded: true, Text: "fwd", Timestamp: ts},
			},
			wantContain:   []string{"[ID:m4]", "[Tags: Forwarded]", "Sender:"},
			wantMsgMapLen: 1,
			wantMsgMap:    map[string]string{"m4": "fwd"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload, msgMap := buildTGPayload(tc.user, tc.msgs)
			for _, sub := range tc.wantContain {
				if !strings.Contains(payload, sub) {
					t.Errorf("payload = %q, want to contain %q", payload, sub)
				}
			}
			if len(msgMap) != tc.wantMsgMapLen {
				t.Errorf("len(msgMap) = %d, want %d", len(msgMap), tc.wantMsgMapLen)
			}
			for id, wantText := range tc.wantMsgMap {
				m, ok := msgMap[id]
				if !ok {
					t.Errorf("msgMap missing key %q", id)
					continue
				}
				if m.Text != wantText {
					t.Errorf("msgMap[%q].Text = %q, want %q", id, m.Text, wantText)
				}
			}
		})
	}
}

func TestTelegramAdapter_BuildPayload(t *testing.T) {
	t.Parallel()
	user := store.User{Name: "Me", Email: "me@x"}
	msgs := []types.RawMessage{
		{ID: "x1", SenderName: "Bob", Text: "hello", Timestamp: time.Unix(1700000000, 0)},
	}
	gotPayload, gotMap := (telegramAdapter{}).BuildPayload(user, nil, msgs)
	wantPayload, wantMap := buildTGPayload(user, msgs)
	if gotPayload != wantPayload {
		t.Errorf("BuildPayload payload = %q, want %q", gotPayload, wantPayload)
	}
	if len(gotMap) != len(wantMap) {
		t.Errorf("BuildPayload len(msgMap) = %d, want %d", len(gotMap), len(wantMap))
	}
}

func strPtr(s string) *string { return &s }
