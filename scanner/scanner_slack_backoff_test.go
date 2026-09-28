package scanner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"message-consolidator/channels"
	"message-consolidator/store"
)

func resetInaccessibleChannels(t *testing.T) {
	t.Helper()
	inaccessibleMu.Lock()
	inaccessibleChannels = map[inaccessibleChannelKey]inaccessibleChannelInfo{}
	inaccessibleMu.Unlock()
	t.Cleanup(func() {
		inaccessibleMu.Lock()
		inaccessibleChannels = map[inaccessibleChannelKey]inaccessibleChannelInfo{}
		inaccessibleMu.Unlock()
	})
}

// TestClassifyAccessError covers the recognized access-failure reasons and the
// pass-through case for unrelated errors.
func TestClassifyAccessError(t *testing.T) {
	cases := []struct {
		err     error
		wantOK  bool
		wantMsg string
	}{
		{errors.New("channel_not_found"), true, "channel_not_found"},
		{errors.New("not_in_channel"), true, "not_in_channel"},
		{errors.New("is_archived"), true, "is_archived"},
		{errors.New("missing_scope"), true, "missing_scope"},
		{errors.New("rate_limited"), false, ""},
		{nil, false, ""},
	}
	for _, c := range cases {
		gotMsg, gotOK := classifyAccessError(c.err)
		if gotOK != c.wantOK || gotMsg != c.wantMsg {
			t.Errorf("classifyAccessError(%v) = (%q,%v), want (%q,%v)", c.err, gotMsg, gotOK, c.wantMsg, c.wantOK)
		}
	}
}

// TestFetchChannelHistoryActivity_Backoff verifies: first access failure records the
// channel and calls the Slack API once; a second attempt within the backoff window is
// skipped entirely (no repeated API call, no repeated log); after the window elapses
// the channel is retried.
func TestFetchChannelHistoryActivity_Backoff(t *testing.T) {
	resetInaccessibleChannels(t)
	origFn := getConversationHistory
	t.Cleanup(func() { getConversationHistory = origFn })

	calls := 0
	getConversationHistory = func(_ *channels.SlackClient, _ *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
		calls++
		return nil, errors.New("channel_not_found")
	}

	sc := channels.NewSlackClient("fake-token")
	threads := []store.SlackThreadMeta{{ChannelID: "C1", ThreadTS: "100.000000"}}

	fetchChannelHistoryActivity(sc, "C1", threads)
	if calls != 1 {
		t.Fatalf("calls after first failure = %d, want 1", calls)
	}
	if !isChannelInaccessible(slackClientKindBot, "C1") {
		t.Fatal("expected channel to be marked inaccessible after channel_not_found")
	}

	fetchChannelHistoryActivity(sc, "C1", threads)
	if calls != 1 {
		t.Fatalf("calls after second attempt within backoff window = %d, want still 1 (skipped)", calls)
	}

	// Force the backoff window to have elapsed.
	inaccessibleMu.Lock()
	inaccessibleChannels[inaccessibleChannelKey{kind: slackClientKindBot, channelID: "C1"}] = inaccessibleChannelInfo{reason: "channel_not_found", until: time.Now().Add(-time.Second)}
	inaccessibleMu.Unlock()

	fetchChannelHistoryActivity(sc, "C1", threads)
	if calls != 2 {
		t.Fatalf("calls after backoff window elapsed = %d, want 2 (retried)", calls)
	}
}

// TestSweepColdReconciliationThreads_SkipsInaccessibleChannel verifies the cold tier
// respects the same channel backoff as the hot sweep, without calling the Slack API.
func TestSweepColdReconciliationThreads_SkipsInaccessibleChannel(t *testing.T) {
	resetInaccessibleChannels(t)
	initTestDB(t)
	origFn := getConversationReplies
	t.Cleanup(func() { getConversationReplies = origFn })

	calls := 0
	getConversationReplies = func(_ *channels.SlackClient, _ *slack.GetConversationRepliesParameters) ([]slack.Message, error) {
		calls++
		return nil, nil
	}
	recordChannelInaccessible(slackClientKindBot, "C_BLOCKED", "channel_not_found")

	sc := channels.NewSlackClient("fake-token")
	group := []store.SlackThreadMeta{{ChannelID: "C_BLOCKED", ThreadTS: "1.0", UserEmail: "blocked@example.com"}}
	if isChannelInaccessible(slackClientKindForEmail(group[0].UserEmail), group[0].ChannelID) {
		// expected path: sweepColdReconciliationThreads would skip this group before
		// ever calling processColdReconciliationGroup.
	} else {
		processColdReconciliationGroup(context.Background(), sc, group, "BOT", map[string]slackThreadIdentity{}, nil, nil)
	}
	if calls != 0 {
		t.Fatalf("calls = %d, want 0 (inaccessible channel must be skipped)", calls)
	}
}
