package channels

import (
	"testing"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/types"
	waProto "go.mau.fi/whatsmeow/proto/waE2E"
)

func TestIsSystemMessage(t *testing.T) {
	tests := []struct {
		name     string
		msg      *events.Message
		expected bool
	}{
		{
			name: "Normal message",
			msg: &events.Message{
				Message: &waProto.Message{
					Conversation: ptr("Hello"),
				},
				Info: types.MessageInfo{
					PushName: "User",
				},
			},
			expected: false,
		},
		{
			name: "Protocol message (Revoke)",
			msg: &events.Message{
				Message: &waProto.Message{
					ProtocolMessage: &waProto.ProtocolMessage{
						Type: waProto.ProtocolMessage_REVOKE.Enum(),
					},
				},
				Info: types.MessageInfo{
					PushName: "User",
				},
			},
			expected: true,
		},
		{
			name: "SenderKeyDistribution message",
			msg: &events.Message{
				Message: &waProto.Message{
					SenderKeyDistributionMessage: &waProto.SenderKeyDistributionMessage{},
				},
				Info: types.MessageInfo{
					PushName: "User",
				},
			},
			expected: true,
		},
		{
			name: "Peer category message",
			msg: &events.Message{
				Message: &waProto.Message{
					Conversation: ptr("Sync data"),
				},
				Info: types.MessageInfo{
					Category: "peer",
				},
			},
			expected: true,
		},
		{
			name: "Status broadcast message",
			msg: &events.Message{
				Message: &waProto.Message{
					Conversation: ptr("My status update"),
				},
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Sender: waTypes.JID{User: "status", Server: "broadcast"},
					},
				},
			},
			expected: true,
		},
		{
			name: "System notification (No PushName)",
			msg: &events.Message{
				Message: &waProto.Message{
					Conversation: ptr("This is a system message"),
				},
				Info: types.MessageInfo{
					PushName: "",
					MessageSource: types.MessageSource{
						IsFromMe: false,
						IsGroup:  false,
					},
				},
			},
			expected: true,
		},
		{
			name: "Nil message",
			msg:      nil,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSystemMessage(tt.msg); got != tt.expected {
				t.Errorf("isSystemMessage() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}
