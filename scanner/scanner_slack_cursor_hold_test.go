package scanner

import (
	"context"
	"testing"

	"message-consolidator/channels"
	"message-consolidator/store"
	"message-consolidator/types"
)

// TestApplySlackScanResults_CursorHold is the regression suite for the 2026-09-27
// incident: a Slack catch-up scan that timed out mid-AI-analysis must not advance a
// channel's cursor past messages that were never actually analyzed.
func TestApplySlackScanResults_CursorHold(t *testing.T) {
	const email = "cursor-hold@example.com"

	tests := []struct {
		name        string
		ctx         func() (context.Context, func())
		heldChanID  string // "" means no channel is held
		wantAdvance map[string]bool
	}{
		{
			name:       "failed channel held, successful channel advances",
			ctx:        func() (context.Context, func()) { return context.Background(), func() {} },
			heldChanID: "C-FAIL",
			wantAdvance: map[string]bool{
				"C-FAIL": false,
				"C-OK":   true,
			},
		},
		{
			name: "ctx cancelled holds every channel",
			ctx: func() (context.Context, func()) {
				c, cancel := context.WithCancel(context.Background())
				cancel()
				return c, func() {}
			},
			heldChanID: "",
			wantAdvance: map[string]bool{
				"C-FAIL": false,
				"C-OK":   false,
			},
		},
		{
			name:       "clean pass advances every channel",
			ctx:        func() (context.Context, func()) { return context.Background(), func() {} },
			heldChanID: "",
			wantAdvance: map[string]bool{
				"C-FAIL": true,
				"C-OK":   true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			initTestDB(t)

			tracker := newSlackHeldTracker()
			if tc.heldChanID != "" {
				tracker.hold(email, tc.heldChanID)
			}

			newTS := map[string]map[string]string{
				email: {
					"C-FAIL": "1700000200.000000",
					"C-OK":   "1700000300.000000",
				},
			}

			ctx, cancel := tc.ctx()
			defer cancel()
			applySlackScanResults(ctx, newTS, tracker)

			for chanID, wantAdvanced := range tc.wantAdvance {
				got := store.GetLastScan(email, store.SourceSlack, chanID) != ""
				if got != wantAdvanced {
					t.Errorf("channel %s: cursor advanced=%v, want %v", chanID, got, wantAdvanced)
				}
			}
		})
	}
}

// TestMarkSlackScanSuccess_WithheldWhenHeld covers the stamp-suppression half of the
// fix: a user with any held channel must not get a last_success stamp, while an
// unaffected user in the same batch still does.
func TestMarkSlackScanSuccess_WithheldWhenHeld(t *testing.T) {
	initTestDB(t)

	held := store.User{Email: "held-user@example.com"}
	clean := store.User{Email: "clean-user@example.com"}

	tracker := newSlackHeldTracker()
	tracker.hold(held.Email, "C-FAIL")

	markSlackScanSuccess([]store.User{held, clean}, tracker)

	if ts := store.GetLastScan(held.Email, store.SourceSlack, store.ScanTargetLastSuccess); ts != "" {
		t.Errorf("expected no last_success stamp for %s (held cursor), got %q", held.Email, ts)
	}
	if ts := store.GetLastScan(clean.Email, store.SourceSlack, store.ScanTargetLastSuccess); ts == "" {
		t.Errorf("expected last_success stamp for %s", clean.Email)
	}
}

// TestSlackAdapter_AckScanned_HoldsFailedChannelOnly verifies the message-ID → channelID
// bookkeeping AckScanned relies on: only ids belonging to the failed group's channel
// get held, and ok=true never holds anything.
func TestSlackAdapter_AckScanned_HoldsFailedChannelOnly(t *testing.T) {
	sc := channels.NewSlackClient("fake-token")
	const email = "adapter-ack@example.com"

	byChannel := map[string][]types.RawMessage{
		"C-FAIL": {{ID: "1700000200.000000", ChannelID: "C-FAIL"}},
		"C-OK":   {{ID: "1700000300.000000", ChannelID: "C-OK"}},
	}

	tests := []struct {
		name      string
		ackChanID string
		ok        bool
		wantHeld  string // "" means no channel should end up held
	}{
		{name: "failed group holds its own channel", ackChanID: "C-FAIL", ok: false, wantHeld: "C-FAIL"},
		{name: "successful group holds nothing", ackChanID: "C-OK", ok: true, wantHeld: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracker := newSlackHeldTracker()
			adapter := newSlackAdapter(context.Background(), sc, byChannel, tracker)

			var ids []string
			for _, m := range byChannel[tc.ackChanID] {
				ids = append(ids, m.ID)
			}
			adapter.AckScanned(context.Background(), email, ids, tc.ok)

			if tc.wantHeld == "" {
				if tracker.hasHeld(email) {
					t.Errorf("expected no held channel, got held state for %s", email)
				}
				return
			}
			if !tracker.isHeld(email, tc.wantHeld) {
				t.Errorf("expected channel %s held for %s, it was not", tc.wantHeld, email)
			}
		})
	}
}
