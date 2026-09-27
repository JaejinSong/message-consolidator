package scanner

import (
	"encoding/json"
	"fmt"
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"
)

func rawJSON(t *testing.T, id, text string) string {
	t.Helper()
	b, err := json.Marshal(types.RawMessage{ID: id, Text: text})
	if err != nil {
		t.Fatalf("marshal raw message: %v", err)
	}
	return string(b)
}

// TestGroupReplayableByChat_GroupsByChatJID verifies rows for the same chat land in one
// slice, and distinct chats stay separate.
func TestGroupReplayableByChat_GroupsByChatJID(t *testing.T) {
	t.Parallel()
	rows := []store.ReplayableWAMessage{
		{MessageID: "m1", ChatJID: "111@s.whatsapp.net", RawJSON: rawJSON(t, "m1", "hello")},
		{MessageID: "m2", ChatJID: "111@s.whatsapp.net", RawJSON: rawJSON(t, "m2", "world")},
		{MessageID: "m3", ChatJID: "222@g.us", RawJSON: rawJSON(t, "m3", "group msg")},
	}

	byChat, malformed, deferred := groupReplayableByChat(rows)

	if len(malformed) != 0 {
		t.Fatalf("malformed = %v, want none", malformed)
	}
	if deferred != 0 {
		t.Fatalf("deferred = %d, want 0", deferred)
	}
	if len(byChat) != 2 {
		t.Fatalf("byChat has %d chats, want 2", len(byChat))
	}
	if got := len(byChat["111@s.whatsapp.net"]); got != 2 {
		t.Errorf("111@s.whatsapp.net has %d messages, want 2", got)
	}
	if got := len(byChat["222@g.us"]); got != 1 {
		t.Errorf("222@g.us has %d messages, want 1", got)
	}
	if byChat["111@s.whatsapp.net"][0].ID != "m1" || byChat["111@s.whatsapp.net"][1].ID != "m2" {
		t.Errorf("111@s.whatsapp.net ids = %v, want [m1 m2] in order", byChat["111@s.whatsapp.net"])
	}
	for _, raws := range byChat {
		for _, raw := range raws {
			if !raw.IsReplay {
				t.Errorf("message %s IsReplay = false, want true", raw.ID)
			}
		}
	}
}

// TestGroupReplayableByChat_MalformedJSONIsSkippedNotDropped verifies a row whose
// raw_json fails to parse is reported back by ID instead of silently disappearing, so
// the caller can mark it failed and let the retry cap age it out.
func TestGroupReplayableByChat_MalformedJSONIsSkippedNotDropped(t *testing.T) {
	t.Parallel()
	rows := []store.ReplayableWAMessage{
		{MessageID: "good", ChatJID: "111@s.whatsapp.net", RawJSON: rawJSON(t, "good", "hi")},
		{MessageID: "bad", ChatJID: "111@s.whatsapp.net", RawJSON: "{not json"},
	}

	byChat, malformed, _ := groupReplayableByChat(rows)

	if len(malformed) != 1 || malformed[0] != "bad" {
		t.Fatalf("malformed = %v, want [bad]", malformed)
	}
	if got := len(byChat["111@s.whatsapp.net"]); got != 1 {
		t.Fatalf("111@s.whatsapp.net has %d messages, want 1 (malformed row excluded)", got)
	}
}

// TestGroupReplayableByChat_Empty verifies the zero-rows case returns empty, non-nil-vs-nil
// details left to the caller (both are treated as "nothing to replay").
func TestGroupReplayableByChat_Empty(t *testing.T) {
	t.Parallel()
	byChat, malformed, deferred := groupReplayableByChat(nil)
	if len(byChat) != 0 || len(malformed) != 0 || deferred != 0 {
		t.Errorf("byChat=%v malformed=%v deferred=%d, want all empty/zero", byChat, malformed, deferred)
	}
}

// TestGroupReplayableByChat_CapsPerChatKeepingOldest verifies a chat with more than
// waReplayPerChatMax replayable rows keeps only the oldest (rows arrive ts-ascending)
// and reports the remainder as deferred, so a burst never overflows the live ChatBuffer's
// per-chat cap (chatBufCap=200).
func TestGroupReplayableByChat_CapsPerChatKeepingOldest(t *testing.T) {
	t.Parallel()
	const total = waReplayPerChatMax + 5
	rows := make([]store.ReplayableWAMessage, 0, total)
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("m%03d", i)
		rows = append(rows, store.ReplayableWAMessage{
			MessageID: id, ChatJID: "111@s.whatsapp.net", RawJSON: rawJSON(t, id, "hi"),
		})
	}

	byChat, malformed, deferred := groupReplayableByChat(rows)

	if len(malformed) != 0 {
		t.Fatalf("malformed = %v, want none", malformed)
	}
	if deferred != 5 {
		t.Fatalf("deferred = %d, want 5", deferred)
	}
	kept := byChat["111@s.whatsapp.net"]
	if len(kept) != waReplayPerChatMax {
		t.Fatalf("kept = %d, want %d", len(kept), waReplayPerChatMax)
	}
	if kept[0].ID != "m000" || kept[len(kept)-1].ID != fmt.Sprintf("m%03d", waReplayPerChatMax-1) {
		t.Errorf("kept ids = [%s..%s], want oldest-first [m000..m%03d]", kept[0].ID, kept[len(kept)-1].ID, waReplayPerChatMax-1)
	}
}
