package types

import "time"

// RawMessage represents a generic text message extracted from any source (Slack, WhatsApp, etc.)
type RawMessage struct {
	ID        string
	Sender    string
	Text      string
	Timestamp time.Time
	ReplyToID string // ID of the message being replied to (if any)
	ThreadID  string // Slack or Gmail thread ID
	ChannelID string // Slack channel ID or room ID
}
