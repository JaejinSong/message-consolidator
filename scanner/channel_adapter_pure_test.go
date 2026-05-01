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
	triggerOutgoingCompletions(context.Background(), msgs, store.User{Email: "u@x", Name: "Me"}, "telegram", "group")
}

func TestTriggerOutgoingCompletions_NoOutgoingMessages(t *testing.T) {
	origCompletion := completionSvc
	t.Cleanup(func() { completionSvc = origCompletion })

	completionSvc = nil

	msgs := []types.RawMessage{
		{ID: "m2", IsFromMe: false, ReplyToID: ""},
		{ID: "m3", IsFromMe: false, ReplyToID: ""},
	}
	triggerOutgoingCompletions(context.Background(), msgs, store.User{Email: "u@x", Name: "Me"}, "telegram", "group")
}
