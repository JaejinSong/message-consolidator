package types

import (
	"encoding/json"
	"time"
)

type MessageCategory string

const (
	CategoryTask   MessageCategory = "TASK"
	CategoryPolicy MessageCategory = "POLICY"
	CategoryQuery  MessageCategory = "QUERY"
)

// RawMessage represents a generic text message extracted from any source (Slack, WhatsApp, etc.)
type RawMessage struct {
	ID            string
	Sender        string
	Text          string
	Timestamp     time.Time
	ReplyToID     string          //Why: Tracks the original message ID to reconstruct conversation threads during AI-driven task context analysis.
	RepliedToUser string          //Why: Identifies the name or ID of the user being replied to for precise assignee allocation.
	ThreadID      string          //Why: Groups messages by their respective platform threads to ensure the AI considers the full conversational context.
	ChannelID     string          //Why: Identifies the specific communication channel within a workspace to help the user locate the original message if needed.
	Category      MessageCategory `json:"category"`
	Metadata      json.RawMessage `json:"metadata"`
	RelatedMessageID int          `json:"related_message_id,omitempty"`
}

// EnrichedMessage represents a unified message model for task analysis.
// Why: Standardizes cross-channel metadata (WhatsApp, Slack, Email) to provide a consistent schema for AI-driven task extraction.
type EnrichedMessage struct {
	RawContent      string    `json:"raw_content"`
	SourceChannel   string    `json:"source_channel"` // "whatsapp", "slack", "email"
	SenderID        int       `json:"sender_id"`       // Why: Explicit integer conversion for DB identity security.
	SenderName      string    `json:"sender_name"`
	VirtualThreadID string    `json:"virtual_thread_id"`
	Timestamp       time.Time `json:"timestamp"`
}
