package channels

import (
	"testing"
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
				Info: types.MessageInfo{},
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
				Info: types.MessageInfo{},
			},
			expected: true,
		},
		{
			name: "SenderKeyDistribution message",
			msg: &events.Message{
				Message: &waProto.Message{
					SenderKeyDistributionMessage: &waProto.SenderKeyDistributionMessage{},
				},
				Info: types.MessageInfo{},
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
