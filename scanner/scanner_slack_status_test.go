package scanner

import (
	"context"
	"testing"
	"time"

	"message-consolidator/channels"
	"message-consolidator/store"
)

// Why: regression for the 2026-09-17 incident — a scan that completes cleanly must
// stamp last_success for every user so /slack/status can surface staleness.
func TestMarkSlackScanSuccess_StampsAllUsers(t *testing.T) {
	initTestDB(t)

	users := []store.User{
		{Email: "stamp-a@example.com"},
		{Email: "stamp-b@example.com"},
	}
	markSlackScanSuccess(users, nil)

	for _, u := range users {
		ts := store.GetLastScan(u.Email, store.SourceSlack, store.ScanTargetLastSuccess)
		if ts == "" {
			t.Errorf("expected last_success stamp for %s", u.Email)
		}
	}
}

// Why: collectSlackHistory's fetchOK signals whether any channel-level fetch failed;
// the trivial empty-channel case must report a clean pass (no channels to fail on).
func TestCollectSlackHistory_EmptyChannelsIsFetchOK(t *testing.T) {
	t.Parallel()
	sc := channels.NewSlackClient("fake-token")
	_, _, fetchOK, _ := collectSlackHistory(context.Background(), nil, nil, sc, nil)
	if !fetchOK {
		t.Error("expected fetchOK=true when there are no channels to scan")
	}
}

// Why: the zero-membership condition (bot removed from every channel) must rate-limit
// its error log to once per slackNoChannelsLogInterval instead of flooding every cycle.
func TestWarnSlackNoChannels_RateLimited(t *testing.T) {
	slackNoChannelsLogMu.Lock()
	slackNoChannelsLoggedAt = time.Time{}
	slackNoChannelsLogMu.Unlock()

	sc := channels.NewSlackClient("fake-token")
	ctx := context.Background()

	warnSlackNoChannels(ctx, sc, "BOTID")
	slackNoChannelsLogMu.Lock()
	first := slackNoChannelsLoggedAt
	slackNoChannelsLogMu.Unlock()
	if first.IsZero() {
		t.Fatal("expected slackNoChannelsLoggedAt to be set after first warning")
	}

	warnSlackNoChannels(ctx, sc, "BOTID")
	slackNoChannelsLogMu.Lock()
	second := slackNoChannelsLoggedAt
	slackNoChannelsLogMu.Unlock()
	if !second.Equal(first) {
		t.Error("expected the second call within the interval to be suppressed (timestamp unchanged)")
	}
}
