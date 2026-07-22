package scanner

import (
	"context"
	"testing"

	"message-consolidator/store"
	"message-consolidator/types"
)

func TestIsIgnorableChannelNoise_NilFilter(t *testing.T) {
	origFilter := filterSvc
	t.Cleanup(func() { filterSvc = origFilter })

	filterSvc = nil

	got := isIgnorableChannelNoise(context.Background(), "u@x", "telegram", "payload", "TG")
	if got != false {
		t.Errorf("isIgnorableChannelNoise() = %v, want false with nil filterSvc", got)
	}
}

func TestTriggerOutgoingCompletions_NilCompletionSvc(t *testing.T) {
	origCompletion := completionSvc
	t.Cleanup(func() { completionSvc = origCompletion })

	completionSvc = nil

	msgs := []types.RawMessage{
		{ID: "m1", IsFromMe: true, ReplyToID: "parent"},
	}
	triggerOutgoingCompletions(context.Background(), msgs, store.User{Email: "u@x", Name: "Me"}, telegramAdapter{}, "group")
}

func TestCompletionDispatchKind(t *testing.T) {
	user := store.User{Email: "u@x", Name: "Me"}
	cases := []struct {
		name string
		msg  types.RawMessage
		want dispatchKind
	}{
		{"fromMe quoted reply is threaded", types.RawMessage{IsFromMe: true, ReplyToID: "parent", Text: "hi"}, dispatchThreaded},
		{"fromMe plain with signal is crossChannel", types.RawMessage{IsFromMe: true, Text: "완료했습니다"}, dispatchCrossChannel},
		{"counterparty with signal is crossChannel", types.RawMessage{IsFromMe: false, Text: "done with this"}, dispatchCrossChannel},
		{"counterparty without signal is none", types.RawMessage{IsFromMe: false, Text: "any update?"}, dispatchNone},
		{"fromMe plain without signal is none", types.RawMessage{IsFromMe: true, Text: "hello there"}, dispatchNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := completionDispatchKind(c.msg, user, telegramAdapter{}); got != c.want {
				t.Errorf("completionDispatchKind() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTriggerOutgoingCompletions_NoOutgoingMessages(t *testing.T) {
	origCompletion := completionSvc
	t.Cleanup(func() { completionSvc = origCompletion })

	completionSvc = nil

	msgs := []types.RawMessage{
		{ID: "m2", IsFromMe: false, ReplyToID: ""},
		{ID: "m3", IsFromMe: false, ReplyToID: ""},
	}
	triggerOutgoingCompletions(context.Background(), msgs, store.User{Email: "u@x", Name: "Me"}, telegramAdapter{}, "group")
}
