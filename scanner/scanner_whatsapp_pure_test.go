package scanner

import (
	"context"
	"strings"
	"testing"
	"time"

	"message-consolidator/config"
	"message-consolidator/store"
	"message-consolidator/types"
)

// initTestDB initialises an in-memory SQLite DB for tests that call store functions.
// Why: buildWAPayload calls store.GetNameByWhatsAppNumber which queries the DB;
// without a valid *sql.DB the call panics even when the store returns an empty string.
func initTestDB(t *testing.T) {
	t.Helper()
	store.ResetForTest()
	if err := store.InitDB(context.Background(), &config.Config{}); err != nil {
		t.Fatalf("initTestDB: %v", err)
	}
}

func TestWhatsAppAdapter_Source(t *testing.T) {
	t.Parallel()
	if got := (whatsAppAdapter{}).Source(); got != "whatsapp" {
		t.Errorf("Source() = %q, want %q", got, "whatsapp")
	}
}

func TestWhatsAppAdapter_LogPrefix(t *testing.T) {
	t.Parallel()
	if got := (whatsAppAdapter{}).LogPrefix(); got != "WA" {
		t.Errorf("LogPrefix() = %q, want %q", got, "WA")
	}
}

func TestWhatsAppAdapter_Is1To1(t *testing.T) {
	t.Parallel()
	tests := []struct {
		roomKey string
		want    bool
	}{
		{"1234567890@s.whatsapp.net", true},
		{"groupid-12345@g.us", false},
		{"", true},
		{"abc@g.us", false},
		{"user@g.us-extra", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.roomKey, func(t *testing.T) {
			t.Parallel()
			if got := (whatsAppAdapter{}).Is1To1(tt.roomKey); got != tt.want {
				t.Errorf("Is1To1(%q) = %v, want %v", tt.roomKey, got, tt.want)
			}
		})
	}
}

func TestBuildWAPayloadPure(t *testing.T) {
	initTestDB(t)
	ts := time.Unix(1700000000, 0)
	tests := []struct {
		name          string
		user          store.User
		msgs          []types.RawMessage
		wantContain   []string
		wantMsgMapLen int
	}{
		{
			name:          "single message uses Sender fallback",
			user:          store.User{Name: "Me", Email: "me@x"},
			msgs:          []types.RawMessage{{ID: "m1", Sender: "+15551234", Text: "hi", Timestamp: ts}},
			wantContain:   []string{"[ID:m1]", "+15551234: hi"},
			wantMsgMapLen: 1,
		},
		{
			name:          "IsFromMe overrides to user.Name",
			user:          store.User{Name: "Owner"},
			msgs:          []types.RawMessage{{ID: "m2", Sender: "+15551234", IsFromMe: true, Text: "self", Timestamp: ts}},
			wantContain:   []string{"[ID:m2]", "Owner: self"},
			wantMsgMapLen: 1,
		},
		{
			name:          "forwarded tag in metadata",
			user:          store.User{Name: "Me"},
			msgs:          []types.RawMessage{{ID: "m3", Sender: "+15551234", IsForwarded: true, Text: "fwd", Timestamp: ts}},
			wantContain:   []string{"[ID:m3]", "[Tags: Forwarded]", "+15551234: fwd"},
			wantMsgMapLen: 1,
		},
		{
			name: "multiple messages preserved",
			user: store.User{Name: "Me"},
			msgs: []types.RawMessage{
				{ID: "a", Sender: "+1", Text: "x", Timestamp: ts},
				{ID: "b", Sender: "+2", Text: "y", Timestamp: ts},
			},
			wantContain:   []string{"[ID:a]", "[ID:b]"},
			wantMsgMapLen: 2,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			payload, msgMap := buildWAPayload(tt.user, nil, tt.msgs)
			for _, sub := range tt.wantContain {
				if !strings.Contains(payload, sub) {
					t.Errorf("payload does not contain %q\npayload: %q", sub, payload)
				}
			}
			if len(msgMap) != tt.wantMsgMapLen {
				t.Errorf("len(msgMap) = %d, want %d", len(msgMap), tt.wantMsgMapLen)
			}
		})
	}
}

func TestFormatWAMentionTagPure(t *testing.T) {
	initTestDB(t)
	email := "test@example.com"
	tests := []struct {
		name         string
		mentionedIDs []string
		wantContain  string
	}{
		{"empty list returns Mentions: 0", []string{}, "Mentions: 0"},
		{"invalid JIDs return Mentions: N", []string{"not-a-jid"}, "Mentions: 1"},
		{"valid JID but no contact match", []string{"1234567890@s.whatsapp.net"}, "Mentions: 1"},
		{"multiple invalid JIDs", []string{"x", "y", "z"}, "Mentions: 3"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := formatWAMentionTag(email, tt.mentionedIDs)
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("formatWAMentionTag() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}

func TestWhatsAppAdapter_BuildPayload(t *testing.T) {
	initTestDB(t)
	ts := time.Unix(1700000000, 0)
	user := store.User{Name: "Alice", Email: "alice@x"}
	msgs := []types.RawMessage{{ID: "x1", Sender: "+9999", Text: "hello", Timestamp: ts}}

	adapterPayload, adapterMap := (whatsAppAdapter{}).BuildPayload(user, nil, msgs)
	directPayload, directMap := buildWAPayload(user, nil, msgs)

	if adapterPayload != directPayload {
		t.Errorf("BuildPayload payload mismatch:\nadapter: %q\ndirect:  %q", adapterPayload, directPayload)
	}
	if len(adapterMap) != len(directMap) {
		t.Errorf("BuildPayload msgMap len: adapter=%d direct=%d", len(adapterMap), len(directMap))
	}
}

func TestWhatsAppAdapter_Enrich_Fallback(t *testing.T) {
	roomKey := "1234@s.whatsapp.net"
	ts := time.Unix(1700000000, 0)

	enriched, err := (whatsAppAdapter{}).Enrich(roomKey, "hello", ts)
	if err != nil {
		t.Fatalf("Enrich() returned error: %v", err)
	}
	if enriched.SourceChannel != "whatsapp" {
		t.Errorf("SourceChannel = %q, want %q", enriched.SourceChannel, "whatsapp")
	}
	if enriched.RawContent != "hello" {
		t.Errorf("RawContent = %q, want %q", enriched.RawContent, "hello")
	}
	if enriched.SenderName != roomKey {
		t.Errorf("SenderName = %q, want %q (fallback)", enriched.SenderName, roomKey)
	}
	wantPrefix := "wa_thread_" + roomKey + "_"
	if !strings.HasPrefix(enriched.VirtualThreadID, wantPrefix) {
		t.Errorf("VirtualThreadID = %q, want prefix %q", enriched.VirtualThreadID, wantPrefix)
	}
}
