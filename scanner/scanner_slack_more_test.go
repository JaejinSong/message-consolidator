package scanner

import (
	"context"
	"sync"
	"testing"

	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/store"
)

// TestHandleThreadTimeout_NonEmptyThreadTS covers the PostMessage + CloseTargetedThread path.
// Why: PostMessage errors are discarded (_,_,_ =), so the fake token fails silently.
func TestHandleThreadTimeout_NonEmptyThreadTS(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "1700000100.000000",
		UserEmail: "timeout-real@example.com",
	}
	// PostMessage will fail (fake token) but error is ignored; CloseTargetedThread uses DB.
	handleThreadTimeout(context.Background(), sc, thread)
}

// TestUpdateThreadStatus_Resolved_EmptyThreadTS covers the resolved + empty-ThreadTS branch.
func TestUpdateThreadStatus_Resolved_EmptyThreadTS(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "",
		UserEmail: "resolve-empty@example.com",
	}
	res := threadScanResult{
		isResolved: true, newLastTS: "1700000200.000000", newLastActivity: "1700000150.000000",
	}
	// empty ThreadTS: warns + closes without PostMessage.
	updateThreadStatus(context.Background(), sc, thread, res)
}

// TestUpdateThreadStatus_Resolved_NonEmptyThreadTS covers PostMessage (fake) + close.
func TestUpdateThreadStatus_Resolved_NonEmptyThreadTS(t *testing.T) {
	initTestDB(t)
	sc := channels.NewSlackClient("fake-token")
	thread := store.SlackThreadMeta{
		ChannelID: "C1", ThreadTS: "1700000100.000000",
		UserEmail: "resolve-real@example.com",
	}
	res := threadScanResult{
		isResolved: true, newLastTS: "1700000200.000000", newLastActivity: "1700000150.000000",
	}
	// PostMessage fails silently; CloseTargetedThread uses DB.
	updateThreadStatus(context.Background(), sc, thread, res)
}

// TestSweepSlackThreads_WithThreads exercises the post-empty guard path.
// Why: register a thread in the DB then run sweep with a fake token.
// The sweep will attempt conversations.replies which fails, but buildSlackAliasCache
// and shouldSkipThreadFetch paths are exercised.
func TestSweepSlackThreads_WithThreads(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	cfg = &config.Config{SlackToken: "xoxb-fake-token-sweep"}

	ctx := context.Background()
	u, _ := store.GetOrCreateUser(ctx, "sweep@example.com", "Sweep User", "")
	if u == nil {
		t.Fatal("GetOrCreateUser returned nil")
	}

	// Register a thread so GetTargetedActiveThreads returns non-empty.
	_ = store.RegisterTargetedSlackThread(ctx, "C_SWEEP", "1700000100.000000", "1700000100.000000", "sweep@example.com")

	wg := &sync.WaitGroup{}
	sweepSlackThreads(ctx, wg)
	wg.Wait()
}

// TestRunSlackForAllUsers_WithDB_EmptyUsers exercises the empty-users early return
// (DB initialised but no slack users).
func TestRunSlackForAllUsers_WithDB_EmptyToken(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	cfg = &config.Config{SlackToken: ""}

	wg := &sync.WaitGroup{}
	runSlackForAllUsers(context.Background(), wg)
	wg.Wait()
}

// TestRunSlackForAllUsers_WithToken_EmptyDB exercises the no-users path past the token guard.
func TestRunSlackForAllUsers_WithToken_EmptyDB(t *testing.T) {
	initTestDB(t)
	saveScannerGlobals(t)
	cfg = &config.Config{SlackToken: "xoxb-fake-token"}

	// GetAllUsers returns empty → scanSlack called with empty users → returns early.
	wg := &sync.WaitGroup{}
	runSlackForAllUsers(context.Background(), wg)
	wg.Wait()
}
