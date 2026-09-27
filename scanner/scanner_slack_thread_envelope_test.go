package scanner

import (
	"testing"

	"github.com/slack-go/slack"
	"message-consolidator/channels"
	"message-consolidator/store"
)

func testThreadMeta() store.SlackThreadMeta {
	return store.SlackThreadMeta{ChannelID: "C08P27C7AC8", ThreadTS: "1789461410.169779", UserEmail: "u@x"}
}

func testThreadReply() slack.Message {
	return slack.Message{Msg: slack.Msg{
		Timestamp:       "1789546445.856419",
		ThreadTimestamp: "1789461410.169779",
		User:            "USLACK",
		Text:            "runbook is up, four tasks in priority order",
	}}
}

// Why: an empty Room made validateTargetTask reject every sweeper-originated update
// as a cross-room operation, so thread replies never closed or updated their task.
func TestBuildThreadCompletionEnvelope_CarriesRoom(t *testing.T) {
	user := &store.User{Email: "u@x", Name: "Me", SlackID: "USLACK"}

	env := BuildThreadCompletionEnvelope(user, testThreadMeta(), testThreadReply(), "biz-global-tech", "Me", true)

	if env.Room != "biz-global-tech" {
		t.Fatalf("Room = %q, want %q", env.Room, "biz-global-tech")
	}
	if env.Source != store.SourceSlack {
		t.Errorf("Source = %q, want %q", env.Source, store.SourceSlack)
	}
	if env.UserEmail != "u@x" {
		t.Errorf("UserEmail = %q, want %q", env.UserEmail, "u@x")
	}
}

func TestBuildThreadCompletionEnvelope_CarriesThreadAndLink(t *testing.T) {
	user := &store.User{Email: "u@x", Name: "Me", SlackID: "USLACK"}
	meta := testThreadMeta()
	m := testThreadReply()

	env := BuildThreadCompletionEnvelope(user, meta, m, "biz-global-tech", "Me", true)

	if env.ThreadID != meta.ThreadTS {
		t.Errorf("ThreadID = %q, want %q", env.ThreadID, meta.ThreadTS)
	}
	if env.RepliedToID != meta.ThreadTS {
		t.Errorf("RepliedToID = %q, want %q", env.RepliedToID, meta.ThreadTS)
	}
	if env.SourceTS != m.Timestamp {
		t.Errorf("SourceTS = %q, want %q", env.SourceTS, m.Timestamp)
	}
	if env.OriginalText != m.Text {
		t.Errorf("OriginalText = %q, want %q", env.OriginalText, m.Text)
	}
	want := "https://slack.com/archives/C08P27C7AC8/p1789546445856419?thread_ts=1789461410.169779"
	if env.Link != want {
		t.Errorf("Link = %q, want %q", env.Link, want)
	}
	if len(env.SourceChannels) != 1 || env.SourceChannels[0] != store.SourceSlack {
		t.Errorf("SourceChannels = %v, want [%s]", env.SourceChannels, store.SourceSlack)
	}
	if ts := channels.ParseSlackTimestamp(m.Timestamp); !env.AssignedAt.Equal(ts) || !env.CreatedAt.Equal(ts) {
		t.Errorf("AssignedAt/CreatedAt = %v/%v, want %v", env.AssignedAt, env.CreatedAt, ts)
	}
}

// Why: RequesterCanonical claims the reply as the user's own; a counterparty reply
// must not be attributed to them (mirrors dispatchSlackCrossChannelCompletion).
func TestBuildThreadCompletionEnvelope_RequesterCanonicalOnlyWhenFromMe(t *testing.T) {
	user := &store.User{Email: "u@x", Name: "Me", SlackID: "USLACK"}

	mine := BuildThreadCompletionEnvelope(user, testThreadMeta(), testThreadReply(), "biz-global-tech", "Me", true)
	if mine.RequesterCanonical != "u@x" {
		t.Errorf("fromMe RequesterCanonical = %q, want %q", mine.RequesterCanonical, "u@x")
	}
	if mine.Requester != "Me" {
		t.Errorf("fromMe Requester = %q, want %q", mine.Requester, "Me")
	}

	theirs := BuildThreadCompletionEnvelope(user, testThreadMeta(), testThreadReply(), "biz-global-tech", "Yoga Wiranda", false)
	if theirs.RequesterCanonical != "" {
		t.Errorf("counterparty RequesterCanonical = %q, want empty", theirs.RequesterCanonical)
	}
	if theirs.Requester != "Yoga Wiranda" {
		t.Errorf("counterparty Requester = %q, want %q", theirs.Requester, "Yoga Wiranda")
	}
	if theirs.Room != "biz-global-tech" {
		t.Errorf("counterparty Room = %q, want %q", theirs.Room, "biz-global-tech")
	}
}
