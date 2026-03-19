package main

import (
	"time"
)

// RawChatMessage represents a raw message from any source (Slack, WhatsApp, Gmail)
type RawChatMessage struct {
	ID             string
	User           string
	Sender         string
	InteractedUser string 
	Text           string
	Timestamp      time.Time
	Time           time.Time // Compatibility with WhatsApp
	RawTS          string
}

// ConsolidatedMessage represents a normalized task message stored in DB
type ConsolidatedMessage struct {
	ID           int        `json:"id"`
	UserEmail    string     `json:"user_email"`
	Source       string     `json:"source"`
	Room         string     `json:"room"`
	Task         string     `json:"task"`
	Requester    string     `json:"requester"`
	Assignee     string     `json:"assignee"`
	AssignedAt   string     `json:"assigned_at"`
	Link         string     `json:"link"`
	SourceTS     string     `json:"source_ts"`
	OriginalText string     `json:"original_text"`
	Done         bool       `json:"done"`
	IsDeleted    bool       `json:"is_deleted"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

// User represents an application user
type User struct {
	ID        int       `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	SlackID   string    `json:"slack_id"`
	WAJID     string    `json:"wa_jid"`
	Picture   string    `json:"picture"`
	Aliases   []string  `json:"aliases"`
	CreatedAt time.Time `json:"created_at"`
}

// TaskTranslation represents a cached translation for a task
type TaskTranslation struct {
	MessageID      int    `json:"message_id"`
	Language       string `json:"language"`
	TranslatedText string `json:"translated_text"`
}

// UserAlias represents a name alias for a user
type UserAlias struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	AliasName string `json:"alias_name"`
}

// TodoItem is the task structure returned by Gemini Analyze
type TodoItem struct {
	Task         string `json:"task"`
	Requester    string `json:"requester"`
	Assignee     string `json:"assignee"`
	AssignedAt   string `json:"assigned_at"`
	SourceTS     string `json:"source_ts"`
	OriginalText string `json:"original_text"`
}

// TranslateRequest represents a request to translate a specific task
type TranslateRequest struct {
	ID           int    `json:"id"`
	Text         string `json:"text"`
	OriginalText string `json:"original_text"`
}

// TranslateResponse represents the batch translation response from Gemini
type TranslateResponse struct {
	Translations []TranslateRequest `json:"translations"`
}
