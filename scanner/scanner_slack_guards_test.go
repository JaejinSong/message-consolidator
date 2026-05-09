package scanner

import (
	"context"
	"testing"

	"github.com/slack-go/slack"
	"message-consolidator/channels"
	"message-consolidator/store"
	"message-consolidator/types"
)

// TestDispatchOutgoingCompletionIfMine_NilSvc covers the completionSvc==nil guard.
func TestDispatchOutgoingCompletionIfMine_NilSvcGuard(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	u := store.User{Email: "u@x", Name: "Me"}
	m := types.RawMessage{ID: "m1", Sender: "Me", ReplyToID: "parent"}
	// Should return immediately without panic.
	dispatchOutgoingCompletionIfMine(context.Background(), sc, u, m)
}

// TestDispatchOutgoingCompletionIfMine_NoReplyToID covers the empty-ReplyToID guard.
func TestDispatchOutgoingCompletionIfMine_NoReplyToIDGuard(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	u := store.User{Email: "u@x", Name: "Me"}
	m := types.RawMessage{ID: "m1", Sender: "Me", ReplyToID: ""}
	dispatchOutgoingCompletionIfMine(context.Background(), sc, u, m)
}

// TestDispatchOutgoingCompletionIfMine_SenderMismatch covers the sender-not-me guard.
func TestDispatchOutgoingCompletionIfMine_SenderMismatch(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil // ensures we don't need a real CompletionService

	sc := channels.NewSlackClient("fake-token")
	u := store.User{Email: "u@x", Name: "Me"}
	m := types.RawMessage{ID: "m1", Sender: "OtherUser", ReplyToID: "parent"}
	dispatchOutgoingCompletionIfMine(context.Background(), sc, u, m)
}

// TestDispatchThreadCompletionIfMine_NilSvc covers the completionSvc==nil guard.
func TestDispatchThreadCompletionIfMine_NilSvc(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	user := &store.User{Email: "u@x", Name: "Me", SlackID: "USLACK"}
	thread := store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "1700000100.000000"}
	m := slack.Message{Msg: slack.Msg{Timestamp: "1700000200.000000", ThreadTimestamp: "1700000100.000000", User: "USLACK"}}
	dispatchThreadCompletionIfMine(context.Background(), sc, user, thread, m)
}

// TestDispatchThreadCompletionIfMine_EmptyThreadTS covers the ThreadTimestamp==empty guard.
func TestDispatchThreadCompletionIfMine_EmptyThreadTS(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	user := &store.User{Email: "u@x", Name: "Me"}
	thread := store.SlackThreadMeta{ChannelID: "C1", ThreadTS: "1700000100.000000"}
	m := slack.Message{Msg: slack.Msg{Timestamp: "1700000200.000000", ThreadTimestamp: "", User: "USLACK"}}
	dispatchThreadCompletionIfMine(context.Background(), sc, user, thread, m)
}

// TestClassifyAndCollect_OlderThanLastTS verifies messages older than lastTS are skipped.
func TestClassifyAndCollect_OlderThanLastTS(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	users := []store.User{{Email: "cat-test@example.com", Name: "Cat User"}}
	c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C1"}}}
	m := types.RawMessage{ID: "1700000100.000000", Sender: "OtherUser", Text: "old msg"}

	// Set lastTS = same as m.ID → msg <= lastTS → skipped for all users.
	_ = store.UpdateLastScan("cat-test@example.com", "slack", "C1", "1700000200.000000")

	candidates := map[string][]types.RawMessage{}
	newTS := map[string]map[string]string{}
	ua := map[string][]string{"cat-test@example.com": {"alias"}}
	classifyAndCollect(context.Background(), c, sc, m, users, ua, candidates, newTS)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates (older than lastTS), got %d", len(candidates))
	}
}

// TestClassifyAndCollect_NewMessage verifies a new message is classified and collected.
func TestClassifyAndCollect_NewMessage(t *testing.T) {
	orig := completionSvc
	t.Cleanup(func() { completionSvc = orig })
	completionSvc = nil

	sc := channels.NewSlackClient("fake-token")
	users := []store.User{{Email: "newmsg@example.com", Name: "New User", SlackID: "UNEW"}}
	// IM channel: isBroadcastChannel → CategoryTask for all messages
	c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "DIMCHAN", IsIM: true}}}
	m := types.RawMessage{ID: "1700000300.000000", Sender: "OtherUser", Text: "hello"}

	// No lastTS set → any message is new.
	_ = store.UpdateLastScan("newmsg@example.com", "slack", "DIMCHAN", "")

	candidates := map[string][]types.RawMessage{}
	newTS := map[string]map[string]string{}
	ua := map[string][]string{"newmsg@example.com": {}}
	classifyAndCollect(context.Background(), c, sc, m, users, ua, candidates, newTS)

	if len(candidates["newmsg@example.com"]) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(candidates["newmsg@example.com"]))
	}
}

// TestScanSlack_GuardNilCfg exercises the nil-cfg guard (15.4% already covers the guard path).
func TestScanSlack_GuardNoCfgOrToken(t *testing.T) {
	saveScannerGlobals(t)

	// cfg nil
	cfg = nil
	scanSlack(context.Background(), nil, nil)
}
