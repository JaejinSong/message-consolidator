package scanner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"message-consolidator/channels"
	"message-consolidator/store"
)

// resetColdTierGate restores the cold-tier cadence gate to its zero state so tests do
// not leak state into each other via the package-level lastColdTierRun guard.
func resetColdTierGate(t *testing.T) {
	t.Helper()
	coldTierMu.Lock()
	orig := lastColdTierRun
	lastColdTierRun = time.Time{}
	coldTierMu.Unlock()
	t.Cleanup(func() {
		coldTierMu.Lock()
		lastColdTierRun = orig
		coldTierMu.Unlock()
	})
}

// TestShouldRunColdTier_CadenceGate verifies the 173-minute in-memory gate: first call
// claims the slot, an immediate second call is refused, and a call past the interval
// is allowed again.
func TestShouldRunColdTier_CadenceGate(t *testing.T) {
	resetColdTierGate(t)

	now := time.Now()
	if !shouldRunColdTier(now) {
		t.Fatal("expected first call to claim the slot and run")
	}
	if shouldRunColdTier(now.Add(time.Minute)) {
		t.Fatal("expected call within the 173-minute window to be skipped")
	}
	if !shouldRunColdTier(now.Add(coldTierInterval + time.Second)) {
		t.Fatal("expected call after the window elapsed to run")
	}
}

// TestProcessColdReconciliationGroup_UnresolvedTouchesCursorOnly verifies the cold tier
// runs through fetchAndDispatchThreadGroup (the same fetch+dispatch path the hot sweep
// uses via processSlackThreadGroup) but, when the thread is not resolved, only advances
// the reply cursor via TouchSlackThreadTimestamps and leaves slack_threads status alone.
func TestProcessColdReconciliationGroup_UnresolvedTouchesCursorOnly(t *testing.T) {
	initTestDB(t)
	origFn := getConversationReplies
	t.Cleanup(func() { getConversationReplies = origFn })

	ctx := context.Background()
	_ = store.RegisterTargetedSlackThread(ctx, "C1", "100.000000", "100.000000", "cold@example.com")
	_ = store.CloseTargetedThread(ctx, "C1", "100.000000", "cold@example.com") // simulate the hot-sweep 7-day timeout having already fired

	getConversationReplies = func(_ *channels.SlackClient, _ *slack.GetConversationRepliesParameters) ([]slack.Message, error) {
		return []slack.Message{
			{Msg: slack.Msg{Timestamp: "200.000000", User: "U_OTHER", Text: "still working on it"}},
		}, nil
	}

	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{{ChannelID: "C1", ThreadTS: "100.000000", UserEmail: "cold@example.com"}}
	processColdReconciliationGroup(ctx, sc, group, "BOT", map[string]slackThreadIdentity{}, &sync.WaitGroup{}, nil)

	var status, lastReplyTS string
	row := store.GetDB().QueryRowContext(ctx, `SELECT status, last_reply_ts FROM slack_threads WHERE channel_id = ? AND thread_ts = ?`, "C1", "100.000000")
	if err := row.Scan(&status, &lastReplyTS); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "resolved" {
		t.Errorf("status = %q, want unchanged %q (cold tier must not reactivate hot-sweep tracking)", status, "resolved")
	}
	if lastReplyTS != "200.000000" {
		t.Errorf("last_reply_ts = %q, want %q (cursor should advance so the reply is not reprocessed)", lastReplyTS, "200.000000")
	}
}

// TestProcessColdReconciliationGroup_ResolvedClosesThread verifies a resolved reply
// (white_check_mark reaction) still runs through the shared dispatch path and closes
// the thread via updateThreadStatusGroup, same as the hot sweep would.
func TestProcessColdReconciliationGroup_ResolvedClosesThread(t *testing.T) {
	initTestDB(t)
	origFn := getConversationReplies
	t.Cleanup(func() { getConversationReplies = origFn })

	ctx := context.Background()
	_ = store.RegisterTargetedSlackThread(ctx, "C2", "300.000000", "300.000000", "cold2@example.com")
	_ = store.CloseTargetedThread(ctx, "C2", "300.000000", "cold2@example.com")

	getConversationReplies = func(_ *channels.SlackClient, _ *slack.GetConversationRepliesParameters) ([]slack.Message, error) {
		return []slack.Message{
			{Msg: slack.Msg{Timestamp: "400.000000", User: "U_OTHER", Text: "done", Reactions: []slack.ItemReaction{{Name: "white_check_mark"}}}},
		}, nil
	}

	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{{ChannelID: "C2", ThreadTS: "300.000000", UserEmail: "cold2@example.com"}}
	processColdReconciliationGroup(ctx, sc, group, "BOT", map[string]slackThreadIdentity{}, &sync.WaitGroup{}, nil)

	var status string
	row := store.GetDB().QueryRowContext(ctx, `SELECT status FROM slack_threads WHERE channel_id = ? AND thread_ts = ?`, "C2", "300.000000")
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "resolved" {
		t.Errorf("status = %q, want %q", status, "resolved")
	}
}
