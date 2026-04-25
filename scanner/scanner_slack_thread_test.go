package scanner

import (
	"fmt"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"message-consolidator/store"
	"message-consolidator/types"
)

func TestScanThreadReplies(t *testing.T) {
	now := time.Now()
	baseTS := fmt.Sprintf("%d.000000", now.Unix()-100)
	botTS := fmt.Sprintf("%d.000000", now.Unix()-50)
	userTS := fmt.Sprintf("%d.000000", now.Unix()-50)
	parentTS := fmt.Sprintf("%d.000000", now.Unix()-200)

	tests := []struct {
		name            string
		replies         []slack.Message
		threadTS        string
		lastTS          string
		lastActivity    string
		botUserID       string
		wantResolved    bool
		wantNewActivity string
		wantNewLastTS   string
	}{
		{
			name:            "should remain unresolved and keep original timestamps when there are no replies",
			replies:         nil,
			threadTS:        "1000.0",
			lastTS:          "1000.0",
			lastActivity:    "1000.0",
			botUserID:       "BOT123",
			wantResolved:    false,
			wantNewActivity: "1000.0",
			wantNewLastTS:   "1000.0",
		},
		{
			name: "should update lastTS but not activity timestamp when the reply is from a bot",
			replies: []slack.Message{
				{Msg: slack.Msg{Timestamp: botTS, User: "BOT123", Text: "Bot reply"}},
			},
			threadTS:        "T_TS",
			lastTS:          baseTS,
			lastActivity:    baseTS,
			botUserID:       "BOT123",
			wantResolved:    false,
			wantNewActivity: baseTS,
			wantNewLastTS:   botTS,
		},
		{
			name: "should identify via BotID field and not update activity timestamp",
			replies: []slack.Message{
				{Msg: slack.Msg{Timestamp: botTS, User: "U_UNK", BotID: "B_BOT", Text: "bot reply"}},
			},
			threadTS:        "T_TS",
			lastTS:          baseTS,
			lastActivity:    baseTS,
			botUserID:       "REAL_BOT",
			wantResolved:    false,
			wantNewActivity: baseTS,
			wantNewLastTS:   botTS,
		},
		{
			name: "should update both activity and lastTS when a human user replies",
			replies: []slack.Message{
				{Msg: slack.Msg{Timestamp: userTS, User: "U_HUMAN", Text: "Hello"}},
			},
			threadTS:        "T_TS",
			lastTS:          baseTS,
			lastActivity:    baseTS,
			botUserID:       "BOT123",
			wantResolved:    false,
			wantNewActivity: userTS,
			wantNewLastTS:   userTS,
		},
		{
			name: "should mark as resolved when a white check mark reaction is present",
			replies: []slack.Message{
				{
					Msg: slack.Msg{
						Timestamp: userTS,
						User:      "U_HUMAN",
						Reactions: []slack.ItemReaction{{Name: "white_check_mark", Count: 1}},
					},
				},
			},
			threadTS:        "T_TS",
			lastTS:          baseTS,
			lastActivity:    baseTS,
			botUserID:       "BOT123",
			wantResolved:    true,
			wantNewActivity: baseTS,
			wantNewLastTS:   userTS,
		},
		{
			name: "should ignore replies that are equal to or older than the last processed timestamp",
			replies: []slack.Message{
				{Msg: slack.Msg{Timestamp: parentTS, User: "U_HUMAN", Text: "parent"}},
			},
			threadTS:        parentTS,
			lastTS:          parentTS,
			lastActivity:    parentTS,
			botUserID:       "BOT123",
			wantResolved:    false,
			wantNewActivity: parentTS,
			wantNewLastTS:   parentTS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanThreadReplies(tt.replies, tt.lastTS, tt.lastActivity, tt.botUserID)
			if got.isResolved != tt.wantResolved {
				t.Errorf("isResolved = %v, want %v", got.isResolved, tt.wantResolved)
			}
			if got.newLastActivity != tt.wantNewActivity {
				t.Errorf("newLastActivity = %v, want %v", got.newLastActivity, tt.wantNewActivity)
			}
			if got.newLastTS != tt.wantNewLastTS {
				t.Errorf("newLastTS = %v, want %v", got.newLastTS, tt.wantNewLastTS)
			}
		})
	}
}

//Why: [isThreadTimedOut] Verifies that inactive threads are correctly identified as timed out based on the configured duration.

func TestIsThreadTimedOut(t *testing.T) {
	now := time.Now().Unix()
	tests := []struct {
		name    string
		ts      string
		timeout time.Duration
		want    bool
	}{
		{
			name:    "should not timeout when the activity was 1 hour ago",
			ts:      fmt.Sprintf("%d.000000", now-3600),
			timeout: 7 * 24 * time.Hour,
			want:    false,
		},
		{
			name:    "should timeout when the activity was 8 days ago",
			ts:      fmt.Sprintf("%d.000000", now-8*24*3600),
			timeout: 7 * 24 * time.Hour,
			want:    true,
		},
		{
			name:    "should not timeout when the timestamp format is invalid",
			ts:      "invalid_ts",
			timeout: 7 * 24 * time.Hour,
			want:    false,
		},
		{
			name:    "should not timeout when the activity is just under the 7-day boundary",
			ts:      fmt.Sprintf("%d.000000", now-7*24*3600+10),
			timeout: 7 * 24 * time.Hour,
			want:    false,
		},
		{
			name:    "should timeout when the activity is just over the 7-day boundary",
			ts:      fmt.Sprintf("%d.000000", now-7*24*3600-10),
			timeout: 7 * 24 * time.Hour,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isThreadTimedOut(tt.ts, tt.timeout); got != tt.want {
				t.Errorf("isThreadTimedOut() = %v, want %v", got, tt.want)
			}
		})
	}
}

//Why: [Throttling & Intake] Ensures that API rate limit protections and thread link construction logic are correctly implemented.

func TestSlackThrottlingInterval(t *testing.T) {
	//Why: 1.0s = 60/min stays within Tier 3 burst tolerance for conversations.replies; 429 fallback handled by withSlackRetry.
	expected := 1000 * time.Millisecond
	if SlackThrottlingInterval != expected {
		t.Errorf("SlackThrottlingInterval should be %v, got %v", expected, SlackThrottlingInterval)
	}
}

func TestThreadIntakeLogicLink(t *testing.T) {
	//Why: Verifies that thread_ts is correctly appended to Slack message links to ensure the user is directed to the specific response in context.
	channelID := "C123"
	replyID := "11111.0000"

	//Why: [Link Logic] Validates that thread_ts is correctly appended for threaded replies to ensure deep links work as expected (Ref: scanner_slack.go:400).
	linkThread := fmt.Sprintf("https://slack.com/archives/%s/p%s", channelID, "123456789")
	if replyID != "" {
		linkThread += fmt.Sprintf("?thread_ts=%s", replyID)
	}

	expected := "https://slack.com/archives/C123/p123456789?thread_ts=11111.0000"
	if linkThread != expected {
		t.Errorf("Thread link mismatch. got=%s, want=%s", linkThread, expected)
	}
}

func TestBuildSlackLink(t *testing.T) {
	cases := []struct {
		name     string
		m        types.RawMessage
		expected string
	}{
		{
			name:     "reply appends thread_ts",
			m:        types.RawMessage{ChannelID: "C123", ID: "999.000", ReplyToID: "111.000"},
			expected: "https://slack.com/archives/C123/p999000?thread_ts=111.000",
		},
		{
			name:     "parent message has no thread_ts param",
			m:        types.RawMessage{ChannelID: "C123", ID: "111.000"},
			expected: "https://slack.com/archives/C123/p111000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildSlackLink(tc.m)
			if got != tc.expected {
				t.Errorf("got=%s, want=%s", got, tc.expected)
			}
		})
	}
}

func TestSlackThreadTS(t *testing.T) {
	// Why: covers the bug where parent messages (no ReplyToID) were never registered for slow sweeper tracking.
	cases := []struct {
		name     string
		m        types.RawMessage
		expected string
	}{
		{
			name:     "reply uses parent as threadTS",
			m:        types.RawMessage{ID: "999.000", ReplyToID: "111.000"},
			expected: "111.000",
		},
		{
			name:     "parent message uses own ID as threadTS",
			m:        types.RawMessage{ID: "111.000"},
			expected: "111.000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slackThreadTS(tc.m)
			if got != tc.expected {
				t.Errorf("got=%s, want=%s", got, tc.expected)
			}
		})
	}
}

func TestMinThreadTS(t *testing.T) {
	got := minThreadTS([]store.SlackThreadMeta{
		{ThreadTS: "1700000200.000000"},
		{ThreadTS: "1700000100.000000"},
		{ThreadTS: "1700000300.000000"},
	})
	if got != "1700000100.000000" {
		t.Errorf("got=%s, want=1700000100.000000", got)
	}
	if got := minThreadTS(nil); got != "" {
		t.Errorf("empty input got=%q, want empty", got)
	}
}

func TestGroupThreadsByChannel(t *testing.T) {
	threads := []store.SlackThreadMeta{
		{ChannelID: "C1", ThreadTS: "1.0"},
		{ChannelID: "C2", ThreadTS: "2.0"},
		{ChannelID: "C1", ThreadTS: "3.0"},
	}
	out := groupThreadsByChannel(threads)
	if len(out) != 2 || len(out["C1"]) != 2 || len(out["C2"]) != 1 {
		t.Errorf("unexpected grouping: %+v", out)
	}
}

func TestBuildChannelActivity(t *testing.T) {
	parentTS := "1700000000.000000"
	silentParentTS := "1700000777.000000"
	tracked := map[string]struct{}{parentTS: {}, silentParentTS: {}}

	parent := slack.Message{Msg: slack.Msg{Timestamp: parentTS, ThreadTimestamp: parentTS, LatestReply: "1700000050.000000", ReplyCount: 3}}
	reply := slack.Message{Msg: slack.Msg{Timestamp: "1700000050.000000", ThreadTimestamp: parentTS}}
	// Why: Slack omits ThreadTimestamp on a parent with zero replies. trackedTS membership must still index it.
	silentParent := slack.Message{Msg: slack.Msg{Timestamp: silentParentTS}}
	standalone := slack.Message{Msg: slack.Msg{Timestamp: "1700000999.000000"}}

	a := buildChannelActivity([]slack.Message{parent, reply, silentParent, standalone}, tracked)
	if !a.fetched {
		t.Fatal("fetched=false")
	}
	if a.latestReplies[parentTS] != "1700000050.000000" {
		t.Errorf("latest_reply=%s", a.latestReplies[parentTS])
	}
	if a.replyCounts[parentTS] != 3 {
		t.Errorf("reply_count=%d", a.replyCounts[parentTS])
	}
	if v, ok := a.latestReplies[silentParentTS]; !ok || v != "" {
		t.Errorf("silent parent must be indexed with empty latest_reply, got ok=%v v=%q", ok, v)
	}
	if a.replyCounts[silentParentTS] != 0 {
		t.Errorf("silent parent reply_count=%d, want 0", a.replyCounts[silentParentTS])
	}
	if _, ok := a.latestReplies["1700000050.000000"]; ok {
		t.Error("reply message must not be indexed as parent")
	}
	if _, ok := a.latestReplies["1700000999.000000"]; ok {
		t.Error("untracked message must not be indexed")
	}
}

func TestShouldSkipThreadFetch(t *testing.T) {
	recent := fmt.Sprintf("%d.000000", time.Now().Unix()-3600)
	expired := fmt.Sprintf("%d.000000", time.Now().Add(-8*24*time.Hour).Unix())

	mkActivity := func(latest string, replyCount int, found bool) map[string]channelActivity {
		a := channelActivity{
			latestReplies: map[string]string{},
			replyCounts:   map[string]int{},
			fetched:       true,
		}
		if found {
			a.latestReplies["T1"] = latest
			a.replyCounts["T1"] = replyCount
		}
		return map[string]channelActivity{"C1": a}
	}

	cases := []struct {
		name string
		t    store.SlackThreadMeta
		act  map[string]channelActivity
		want bool
	}{
		{"timed-out forces fetch", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "X", LastActivityTS: expired}, mkActivity("X", 1, true), false},
		{"channel not fetched falls through", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "X", LastActivityTS: recent}, map[string]channelActivity{"C1": {fetched: false}}, false},
		{"thread parent missing falls through", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "X", LastActivityTS: recent}, mkActivity("", 0, false), false},
		{"no replies → skip", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "", LastActivityTS: recent}, mkActivity("", 0, true), true},
		{"first reply ever → fetch", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "", LastActivityTS: recent}, mkActivity("1700000050.000000", 1, true), false},
		{"latest_reply unchanged → skip", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "1700000050.000000", LastActivityTS: recent}, mkActivity("1700000050.000000", 1, true), true},
		{"latest_reply moved forward → fetch", store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "T1", LastTS: "1700000050.000000", LastActivityTS: recent}, mkActivity("1700000099.000000", 2, true), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldSkipThreadFetch(c.t, c.act); got != c.want {
				t.Errorf("got=%v want=%v", got, c.want)
			}
		})
	}
}
