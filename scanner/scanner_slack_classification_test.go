package scanner

import (
	"message-consolidator/store"
	"message-consolidator/types"
	"testing"

	"github.com/slack-go/slack"
)

func TestClassifyMessage_EdgeCases(t *testing.T) {
	user := &store.User{
		Name:    "Jinro",
		Email:   "jinro@example.com",
		SlackID: "U12345",
	}
	aliases := []string{"진로", "jinro"}

	tests := []struct {
		name     string
		channel  slack.Channel
		message  types.RawMessage
		expected string
	}{
		{
			name:    "IM channel should be classified as 내 업무",
			channel: slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D123", IsIM: true}}},
			message: types.RawMessage{Sender: "U999", Text: "Hello"},
			expected: "내 업무",
		},
		{
			name:    "Mention in public channel should be classified as 내 업무",
			channel: slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message: types.RawMessage{Sender: "U999", Text: "Hey <@U12345> check this"},
			expected: "내 업무",
		},
		{
			name:    "Alias match in public channel should be classified as 내 업무",
			channel: slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message: types.RawMessage{Sender: "U999", Text: "진로님 확인 부탁드려요"},
			expected: "내 업무",
		},
		{
			name:    "Unknown user and no mention should be classified as 기타 업무",
			channel: slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message: types.RawMessage{Sender: "U999", Text: "General message"},
			expected: "기타 업무",
		},
		{
			name:    "Message from me to others (mentions someone else) should be classified as 회신 대기",
			channel: slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message: types.RawMessage{Sender: "Jinro", Text: "<@UOTHERS> please check"},
			expected: "회신 대기",
		},
		{
			name:    "Message from me without other mentions should NOT be classified as 회신 대기",
			channel: slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message: types.RawMessage{Sender: "Jinro", Text: "I will do it"},
			expected: "내 업무",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyMessage(tt.channel, user, aliases, tt.message)
			if got != tt.expected {
				t.Errorf("classifyMessage() = %v, want %v", got, tt.expected)
			}
		})
	}
}
