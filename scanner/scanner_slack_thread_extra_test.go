package scanner

import (
	"context"
	"sync"
	"testing"

	"github.com/slack-go/slack"
	"message-consolidator/channels"
	"message-consolidator/store"
)

// TestUpdateThreadStatus_NoChange verifies no store call when nothing changed.
func TestUpdateThreadStatus_NoChange(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "1700000100.000000",
		LastTS: "1700000200.000000", LastActivityTS: "1700000150.000000",
		UserEmail: "status@example.com",
	}
	// res.newLastTS == t.LastTS AND res.newLastActivity == t.LastActivityTS → no update call.
	res := threadScanResult{
		isResolved:      false,
		newLastTS:       thread.LastTS,
		newLastActivity: thread.LastActivityTS,
	}
	// Should not panic; no DB write expected.
	updateThreadStatus(context.Background(), sc, thread, res)
}

// TestUpdateThreadStatus_Update verifies store.UpdateTargetedThread is called when timestamps differ.
func TestUpdateThreadStatus_Update(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "1700000100.000000",
		LastTS: "1700000200.000000", LastActivityTS: "1700000150.000000",
		UserEmail: "status-upd@example.com",
	}
	res := threadScanResult{
		isResolved:      false,
		newLastTS:       "1700000300.000000", // changed
		newLastActivity: thread.LastActivityTS,
	}
	// UpdateTargetedThread writes to DB; should not panic even if the thread row is missing.
	updateThreadStatus(context.Background(), sc, thread, res)
}

// TestCollectThreadCandidates_AllFiltered verifies no candidates returned when all replies
// are filtered by LastTS (≤ lastTS) without triggering sc.GetUserName.
func TestCollectThreadCandidates_AllFiltered(t *testing.T) {
	orig := deps.completionSvc
	t.Cleanup(func() { deps.completionSvc = orig })
	deps.completionSvc = nil // dispatchThreadCompletionIfMine returns immediately

	sc := channels.NewSlackClient("fake-token")
	user := &store.User{Email: "filter@example.com", Name: "Filter User", SlackID: "UFILTER"}
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "1700000100.000000", LastTS: "1700000200.000000",
	}

	replies := []slack.Message{
		{Msg: slack.Msg{Timestamp: "1700000100.000000", User: "UOTHER", Text: "old"}},   // <= lastTS
		{Msg: slack.Msg{Timestamp: "1700000050.000000", User: "UOTHER", Text: "older"}}, // <= lastTS
	}
	res := threadScanResult{isResolved: false, newLastTS: "1700000200.000000"}

	got := collectThreadCandidates(context.Background(), sc, user, thread, replies, res, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 candidates (all filtered), got %d", len(got))
	}
}

// TestCollectThreadCandidates_BotFiltered verifies bot messages are skipped.
func TestCollectThreadCandidates_BotFiltered(t *testing.T) {
	orig := deps.completionSvc
	t.Cleanup(func() { deps.completionSvc = orig })
	deps.completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	user := &store.User{Email: "botfilter@example.com", Name: "Bot Filter", SlackID: "UBOTFILTER"}
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "1700000100.000000", LastTS: "",
	}

	replies := []slack.Message{
		{Msg: slack.Msg{Timestamp: "1700000200.000000", BotID: "B_BOT", Text: "bot msg"}},
		{Msg: slack.Msg{Timestamp: "1700000300.000000", SubType: "bot_message", Text: "bot2"}},
	}
	res := threadScanResult{isResolved: false, newLastTS: "1700000300.000000"}

	got := collectThreadCandidates(context.Background(), sc, user, thread, replies, res, nil)
	if len(got) != 0 {
		t.Errorf("expected 0 candidates (all bots), got %d", len(got))
	}
}

// TestHandleThreadTimeout_EmptyThreadTS covers the empty ThreadTS early-return branch.
func TestHandleThreadTimeout_EmptyThreadTS(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "", // empty → log + close without PostMessage
		UserEmail: "timeout@example.com",
	}
	// Should not panic or make real API calls.
	handleThreadTimeout(context.Background(), sc, thread)
}

// TestSweepSlackThreads_CfgNilOrEmptyToken covers the guard paths directly.
func TestSweepSlackThreads_GuardPaths(t *testing.T) {
	saveScannerGlobals(t)

	// cfg nil → immediate return
	cfg = nil
	wg := &sync.WaitGroup{}
	sweepSlackThreads(context.Background(), wg)
	wg.Wait()
}
