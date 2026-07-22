package scanner

import (
	"context"
	"testing"
	"time"

	"message-consolidator/db"
	"message-consolidator/services"
	"message-consolidator/store"
)

// Characterization tests pinning the LINE scan pipeline's observable outputs
// (payload text and saved ConsolidatedMessage fields) so the ChannelAdapter
// migration cannot silently shift behavior.

func lineFixtureRows() []db.LineInbox {
	return []db.LineInbox{
		{
			ID: 1, LineMessageID: "lm-100", ChatType: "group", ChatID: "C-grp-1",
			SenderID: "U-alice", SenderName: "Alice", Text: "보고서 내일까지 부탁해요",
			Ts: 1752800000, MentionedIds: "[]",
		},
		{
			ID: 2, LineMessageID: "lm-101", ChatType: "group", ChatID: "C-grp-1",
			SenderID: "U-bob", SenderName: "", Text: "네 알겠습니다",
			Ts: 1752800060, ReplyToID: "lm-100", MentionedIds: "",
		},
	}
}

func TestLinePayloadCharacterization(t *testing.T) {
	rows := lineFixtureRows()
	adapter := newLineAdapter("C-grp-1", "group", "C-grp-1", rows)
	raws := adapter.PopMessages("any")["C-grp-1"]
	payload, rawMsgs := adapter.BuildPayload(store.User{}, nil, raws)

	// Sender chain: SenderName → SenderID → "unknown"; time rendered as local 15:04.
	want := "[ID:lm-100][" + time.Unix(1752800000, 0).Format("15:04") + "] Alice: 보고서 내일까지 부탁해요\n" +
		"[ID:lm-101][" + time.Unix(1752800060, 0).Format("15:04") + "] U-bob: 네 알겠습니다\n"
	if payload != want {
		t.Errorf("payload mismatch\n got: %q\nwant: %q", payload, want)
	}
	if len(rawMsgs) != 2 {
		t.Fatalf("expected 2 raw msgs, got %d", len(rawMsgs))
	}
	if _, ok := rawMsgs["lm-100"]; !ok {
		t.Error("rawMsgs must be keyed by LineMessageID")
	}
}

func TestLineConsolidatedMsgCharacterization(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	user, _ := store.GetOrCreateUser(ctx, "line-char@example.com", "Char User", "")
	if user == nil {
		t.Fatal("GetOrCreateUser returned nil")
	}

	rows := lineFixtureRows()
	roomName := resolveLINERoom("C-grp-1", "group", rows)
	if roomName != "C-grp-1" {
		t.Fatalf("group chat room must stay chatID, got %q", roomName)
	}

	item := store.TodoItem{Task: "보고서 제출", SourceTS: "lm-100", Category: "TASK"}
	adapter := newLineAdapter("C-grp-1", "group", roomName, rows)
	msg := services.BuildTask(ctx, buildChannelTaskParams(ctx, *user, nil, item, lineRowToRaw(rows[0]), roomName, adapter))

	checks := []struct {
		name, got, want string
	}{
		{"Source", msg.Source, store.SourceLine},
		{"Room", msg.Room, "C-grp-1"},
		{"ThreadID", msg.ThreadID, "lm-100"},
		{"SourceTS", msg.SourceTS, "lm-100"},
		{"OriginalText", msg.OriginalText, "보고서 내일까지 부탁해요"},
		{"Task", msg.Task, "보고서 제출"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if len(msg.SourceChannels) != 1 || msg.SourceChannels[0] != store.SourceLine {
		t.Errorf("SourceChannels = %v, want [line]", msg.SourceChannels)
	}
	if !msg.AssignedAt.Equal(time.Unix(1752800000, 0)) {
		t.Errorf("AssignedAt = %v, want %v", msg.AssignedAt, time.Unix(1752800000, 0))
	}
	// Requester resolution on a clean DB: falls back to the raw sender display name.
	if msg.Requester != "Alice" {
		t.Errorf("Requester = %q, want %q", msg.Requester, "Alice")
	}
}

func TestLineDMRoomCharacterization(t *testing.T) {
	rows := []db.LineInbox{{
		LineMessageID: "lm-dm-1", ChatType: "user", ChatID: "U-carol",
		SenderID: "U-carol", SenderName: "Carol", Text: "hi", Ts: 1752800000,
	}}
	if got := resolveLINERoom("U-carol", "user", rows); got != "LINE DM: Carol" {
		t.Errorf("DM room = %q, want %q", got, "LINE DM: Carol")
	}
	// Sender name absent and unresolvable → falls back to chatID.
	rowsNoName := []db.LineInbox{{
		LineMessageID: "lm-dm-2", ChatType: "user", ChatID: "U-dave", Text: "hi", Ts: 1752800000,
	}}
	if got := resolveLINERoom("U-dave", "user", rowsNoName); got != "LINE DM: U-dave" {
		t.Errorf("DM room fallback = %q, want %q", got, "LINE DM: U-dave")
	}
}
