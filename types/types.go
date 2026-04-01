package types

import "time"

// RawMessage represents a generic text message extracted from any source (Slack, WhatsApp, etc.)
type RawMessage struct {
	ID        string
	Sender    string
	Text      string
	Timestamp time.Time
	ReplyToID     string    //Why: Tracks the original message ID to reconstruct conversation threads during AI-driven task context analysis.
	RepliedToUser string    //Why: Identifies the name or ID of the user being replied to for precise assignee allocation.
	ThreadID      string    //Why: Groups messages by their respective platform threads to ensure the AI considers the full conversational context.
	ChannelID     string    //Why: Identifies the specific communication channel within a workspace to help the user locate the original message if needed.
}
