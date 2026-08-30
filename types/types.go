package types

import (
	"encoding/json"
	"message-consolidator/internal/ids"
	"strings"
	"time"
)

type MessageCategory string

const (
	CategoryTask    MessageCategory = "TASK"
	CategoryPolicy  MessageCategory = "POLICY"
	CategoryQuery   MessageCategory = "QUERY"
	CategoryPromise MessageCategory = "PROMISE"
	CategoryWaiting MessageCategory = "WAITING"
)

// validTaskCategories is the closed set of AI extraction categories.
var validTaskCategories = map[MessageCategory]struct{}{
	CategoryTask:    {},
	CategoryPolicy:  {},
	CategoryQuery:   {},
	CategoryPromise: {},
	CategoryWaiting: {},
}

// IsValidTaskCategory reports whether s is one of the closed AI extraction categories.
func IsValidTaskCategory(s string) bool {
	_, ok := validTaskCategories[MessageCategory(strings.ToUpper(s))]
	return ok
}

// RawMessage represents a generic text message extracted from any source (Slack, WhatsApp, etc.)
type RawMessage struct {
	ID               string
	Sender           string
	SenderName       string // Display name from the From header, for AI prompt enrichment
	Text             string
	Timestamp        time.Time
	ReplyToID        string          //Why: Tracks the original message ID to reconstruct conversation threads during AI-driven task context analysis.
	RepliedToUser    string          //Why: Identifies the name or ID of the user being replied to for precise assignee allocation.
	ThreadID         string          //Why: Groups messages by their respective platform threads to ensure the AI considers the full conversational context.
	ChannelID        string          //Why: Identifies the specific communication channel within a workspace to help the user locate the original message if needed.
	Category         MessageCategory `json:"category"`
	Metadata         json.RawMessage `json:"metadata"`
	RelatedMessageID int             `json:"related_message_id,omitempty"`

	// Extended Metadata for AI Context Enrichment
	HasAttachment   bool     `json:"has_attachment"`
	AttachmentNames []string `json:"attachment_names"`
	IsFromMe        bool     `json:"is_from_me"`
	IsCcOnly        bool     `json:"is_cc_only"` // Why: User is on Cc but not To/Bcc/From — informational copy, not actor. Drives envelope-based assignee guard in services.BuildTask.
	IsForwarded     bool     `json:"is_forwarded"`
	IsPinned        bool     `json:"is_pinned"`
	IsImportant     bool     `json:"is_important"`
	Reactions       []string `json:"reactions"`
	MentionedIDs    []string `json:"mentioned_ids"`
	MentionedNames  []string `json:"mentioned_names"` // Why: Pre-resolved display names from MentionedIDs; enables pickFirstMentionAssignee fallback without re-querying the channel API.
}

// EnrichedMessage represents a unified message model for task analysis.
// Why: Standardizes cross-channel metadata (WhatsApp, Slack, Email) to provide a consistent schema for AI-driven task extraction.
type EnrichedMessage struct {
	RawContent      string     `json:"raw_content"`
	SourceChannel   string     `json:"source_channel"` // "whatsapp", "slack", "email"
	ChatType        string     `json:"chat_type"`      // Why: "1to1" or "group"; empty for non-chat sources (email). Lets AI apply correct assignee/policy rules without re-inferring from sender count.
	SenderID        ids.UserID `json:"sender_id"`      // Why: Explicit phantom type for DB identity security.
	SenderName      string     `json:"sender_name"`
	VirtualThreadID string     `json:"virtual_thread_id"`
	Timestamp       time.Time  `json:"timestamp"`
}
