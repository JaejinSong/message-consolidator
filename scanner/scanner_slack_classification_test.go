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
		expected types.SlackMsgType
	}{
		{
			name:     "IM channel should be classified as MsgTypeMyTask",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "D123", IsIM: true}}},
			message:  types.RawMessage{Sender: "U999", Text: "Hello"},
			expected: types.MsgTypeMyTask,
		},
		{
			name:     "Mention in public channel should be classified as MsgTypeMyTask",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "U999", Text: "Hey <@U12345> check this"},
			expected: types.MsgTypeMyTask,
		},
		{
			name:     "Alias match in public channel should be classified as MsgTypeMyTask",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "U999", Text: "진로님 확인 부탁드려요"},
			expected: types.MsgTypeMyTask,
		},
		{
			name:     "Group mention @here should be classified as MsgTypeMyTask",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "U999", Text: "<!here> Emergency fix"},
			expected: types.MsgTypeMyTask,
		},
		{
			name:     "Group mention @channel should be classified as MsgTypeMyTask",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "U999", Text: "<!channel> Meeting now"},
			expected: types.MsgTypeMyTask,
		},
		{
			name:     "Unknown user and no mention should be classified as MsgTypeOther",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "U999", Text: "General message"},
			expected: types.MsgTypeOther,
		},
		{
			name:     "Message from me to others (mentions someone else) should be classified as MsgTypeWaiting",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "Jinro", Text: "<@UOTHERS> please check"},
			expected: types.MsgTypeWaiting,
		},
		{
			name:     "Message from me without other mentions should be classified as MsgTypeMyTask (via alias matching sender)",
			channel:  slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C123"}}},
			message:  types.RawMessage{Sender: "Jinro", Text: "I will do it"},
			expected: types.MsgTypeMyTask,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyMessage(tt.channel, user, aliases, tt.message)
			if got != tt.expected {
				t.Errorf("%s: classifyMessage() = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
