package scanner

import (
	"context"
	"testing"

	"github.com/slack-go/slack"
	"message-consolidator/channels"
	"message-consolidator/store"
	"message-consolidator/types"
)

func TestInvolvedThreads(t *testing.T) {
	user := &store.User{Email: "inv@example.com", Name: "Me Myself", SlackID: "UME"}
	aliases := []string{"zebrakeeper"}
	tests := []struct {
		name string
		msg  types.RawMessage
		want string // expected thread ts; empty = not involved
	}{
		{"mention in root", types.RawMessage{ID: "100.1", Sender: "Other", Text: "hi <@UME> please look"}, "100.1"},
		{"own reply in thread", types.RawMessage{ID: "100.3", ReplyToID: "100.0", Sender: "Me Myself", Text: "ok"}, "100.0"},
		{"here is not involvement", types.RawMessage{ID: "100.4", Sender: "Other", Text: "<!here> deploy"}, ""},
		{"alias match", types.RawMessage{ID: "100.5", Sender: "Other", Text: "ask zebrakeeper about it"}, "100.5"},
		{"unrelated", types.RawMessage{ID: "100.6", Sender: "Other", Text: "lunch?"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := involvedThreads(user, aliases, []types.RawMessage{tt.msg})
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("expected no involved threads, got %v", got)
				}
				return
			}
			if !got[tt.want] || len(got) != 1 {
				t.Errorf("expected only %q involved, got %v", tt.want, got)
			}
		})
	}
}

func TestClassifyAndCollect_InvolvementGate(t *testing.T) {
	orig := deps.completionSvc
	t.Cleanup(func() { deps.completionSvc = orig })
	deps.completionSvc = nil

	const email = "gate@example.com"
	sc := channels.NewSlackClient("fake-token")
	users := []store.User{{Email: email, Name: "Gate User", SlackID: "UGATE"}}
	c := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "CPUB"}}}
	_ = store.UpdateLastScan(email, "slack", "CPUB", "")

	tests := []struct {
		name          string
		msg           types.RawMessage
		involved      map[string]map[string]bool
		wantCollected int
	}{
		{"uninvolved thread", types.RawMessage{ID: "1700000400.000000", Sender: "Other", Text: "hello"},
			map[string]map[string]bool{email: {"1700000100.000000": true}}, 0},
		{"involved thread reply", types.RawMessage{ID: "1700000500.000000", ReplyToID: "1700000100.000000", Sender: "Other", Text: "any update?"},
			map[string]map[string]bool{email: {"1700000100.000000": true}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := map[string]map[string][]types.RawMessage{}
			newTS := map[string]map[string]string{}
			ua := map[string][]string{email: {}}
			classifyAndCollect(context.Background(), c, sc, tt.msg, users, ua, tt.involved, candidates, newTS)

			if got := len(candidates[email]["CPUB"]); got != tt.wantCollected {
				t.Errorf("collected = %d, want %d", got, tt.wantCollected)
			}
			if newTS[email]["CPUB"] != tt.msg.ID {
				t.Errorf("cursor = %q, want %q", newTS[email]["CPUB"], tt.msg.ID)
			}
		})
	}
}
