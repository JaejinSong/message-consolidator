package scanner

import (
	"reflect"
	"testing"

	"message-consolidator/store"
	"message-consolidator/types"

	"github.com/slack-go/slack"
)

func TestFilterExcludedSlackChannels(t *testing.T) {
	mk := func(names ...string) []slack.Channel {
		out := make([]slack.Channel, 0, len(names))
		for _, n := range names {
			var c slack.Channel
			c.Name = n
			out = append(out, c)
		}
		return out
	}
	tests := []struct {
		name     string
		chans    []string
		excluded []string
		want     []string
	}{
		{"case insensitive", []string{"MyHermes", "dev"}, []string{"myhermes"}, []string{"dev"}},
		{"hash prefix", []string{"random", "dev"}, []string{"#random"}, []string{"dev"}},
		{"spaces", []string{"random", "dev"}, []string{"  random "}, []string{"dev"}},
		{"empty list", []string{"random", "dev"}, nil, []string{"random", "dev"}},
		{"hash and empty entries keep empty-name IM", []string{"", "dev"}, []string{"#", ""}, []string{"", "dev"}},
		{"no match", []string{"random", "dev"}, []string{"other"}, []string{"random", "dev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := mk(tt.chans...)
			got := filterExcludedSlackChannels(in, tt.excluded)
			var names []string
			for _, c := range got {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, tt.want) {
				t.Errorf("got %v, want %v", names, tt.want)
			}
			if len(in) != len(tt.chans) {
				t.Errorf("input mutated")
			}
		})
	}
}

func TestIsFromUser_SenderID(t *testing.T) {
	user := &store.User{Name: "Jaejin", SlackID: "U123"}
	if !isFromUser(user, types.RawMessage{Sender: "jj.song", SenderID: "U123"}) {
		t.Error("expected match by SenderID despite different display name")
	}
	if isFromUser(user, types.RawMessage{Sender: "other", SenderID: "U999"}) {
		t.Error("unexpected match")
	}
}

func TestInvolvedThreads_SenderID(t *testing.T) {
	user := &store.User{Name: "Jaejin", SlackID: "U123"}
	msgs := []types.RawMessage{
		{ID: "1.0", Sender: "alice", SenderID: "U1", Text: "hi"},
		{ID: "1.1", ReplyToID: "1.0", Sender: "jj.song", SenderID: "U123", Text: "ok"},
		{ID: "2.0", Sender: "bob", SenderID: "U2", Text: "yo"},
	}
	got := involvedThreads(user, nil, msgs)
	if !got["1.0"] || got["2.0"] {
		t.Errorf("got %v", got)
	}
}
