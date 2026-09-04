package scanner

import (
	"context"
	"testing"

	"message-consolidator/store"
)

// TestWhatsAppLegacyRoomName pins the label WhatsApp history sits under, which is what the
// rename repair matches on.
func TestWhatsAppLegacyRoomName(t *testing.T) {
	cases := []struct{ roomKey, want string }{
		{"279516505182402@lid", "279516505182402"},
		{"60122362207@s.whatsapp.net", "60122362207"},
		{"120363000000000000@g.us", "120363000000000000"},
		{"no-at-sign", ""},
	}
	for _, c := range cases {
		if got := (whatsAppAdapter{}).LegacyRoomName(c.roomKey); got != c.want {
			t.Errorf("LegacyRoomName(%q) = %q, want %q", c.roomKey, got, c.want)
		}
	}
}

// stubRenamerAdapter is a minimal ChannelAdapter + RoomRenamer pair for driving the repair.
type stubRenamerAdapter struct {
	whatsAppAdapter
	legacy string
}

func (a stubRenamerAdapter) LegacyRoomName(string) string { return a.legacy }

// TestRepairLegacyRoomName covers the repair end to end against a real DB: rows sitting under
// the numeric label move to the resolved name, the scoping holds, and a second pass is a no-op.
func TestRepairLegacyRoomName(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	const email = "rename-test@example.com"
	if _, err := store.GetOrCreateUser(ctx, email, "Rename User", ""); err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}

	seed := func(room, task, sourceTS string) {
		if _, err := store.GetDB().ExecContext(ctx,
			`INSERT INTO messages (user_email, source, room, task, requester, assignee, source_ts, done, is_deleted)
			 VALUES (?, ?, ?, ?, 'someone', 'shared', ?, 0, 0)`,
			email, store.SourceWhatsApp, room, task, sourceTS); err != nil {
			t.Fatalf("seed %q: %v", room, err)
		}
	}
	countIn := func(room string) int {
		var n int
		if err := store.GetDB().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM messages WHERE user_email = ? AND room = ?`, email, room).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", room, err)
		}
		return n
	}

	seed("279516505182402", "Align AI/ML architecture", "ts-1")
	seed("279516505182402", "Share the checklist", "ts-2")
	// Same numeric label on a different source must not be touched by a WhatsApp repair.
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, requester, assignee, source_ts, done, is_deleted)
		 VALUES (?, 'telegram', '279516505182402', 'Unrelated', 'someone', 'shared', 'ts-3', 0, 0)`,
		email); err != nil {
		t.Fatalf("seed telegram row: %v", err)
	}

	adapter := stubRenamerAdapter{legacy: "279516505182402"}
	repairLegacyRoomName(ctx, email, adapter, "279516505182402@lid", "Sinan Aydemir")

	if got := countIn("Sinan Aydemir"); got != 2 {
		t.Errorf("rows under the resolved name = %d, want 2", got)
	}
	if got := countIn("279516505182402"); got != 1 {
		t.Errorf("rows still under the numeric label = %d, want 1 (the telegram row)", got)
	}

	// Idempotent: nothing left to move, and no error.
	repairLegacyRoomName(ctx, email, adapter, "279516505182402@lid", "Sinan Aydemir")
	if got := countIn("Sinan Aydemir"); got != 2 {
		t.Errorf("second pass changed the row count to %d, want 2", got)
	}
}

// TestRepairLegacyRoomNameSkips covers every guard that must leave the table alone: an
// unresolved name, a name identical to the legacy label, and an adapter that never had a
// weaker fallback.
func TestRepairLegacyRoomNameSkips(t *testing.T) {
	initTestDB(t)
	ctx := context.Background()
	const email = "rename-skip@example.com"
	if _, err := store.GetOrCreateUser(ctx, email, "Skip User", ""); err != nil {
		t.Fatalf("GetOrCreateUser: %v", err)
	}
	if _, err := store.GetDB().ExecContext(ctx,
		`INSERT INTO messages (user_email, source, room, task, requester, assignee, source_ts, done, is_deleted)
		 VALUES (?, ?, '60122362207', 'Keep me', 'someone', 'shared', 'skip-1', 0, 0)`,
		email, store.SourceWhatsApp); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stayed := func(what string) {
		var room string
		if err := store.GetDB().QueryRowContext(ctx,
			`SELECT room FROM messages WHERE user_email = ? AND source_ts = 'skip-1'`, email).Scan(&room); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if room != "60122362207" {
			t.Errorf("%s: room became %q, want it untouched", what, room)
		}
	}

	adapter := stubRenamerAdapter{legacy: "60122362207"}
	repairLegacyRoomName(ctx, email, adapter, "60122362207@s.whatsapp.net", "")
	stayed("unresolved name")

	repairLegacyRoomName(ctx, email, adapter, "60122362207@s.whatsapp.net", "60122362207")
	stayed("name equals the legacy label")

	repairLegacyRoomName(ctx, email, stubRenamerAdapter{legacy: ""}, "60122362207@s.whatsapp.net", "Someone")
	stayed("adapter reports no legacy label")
}
