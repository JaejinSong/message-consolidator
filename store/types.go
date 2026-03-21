package store

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
	OriginalText string     `json:"original_text,omitempty"`
	HasOriginal  bool       `json:"has_original,omitempty"`
	Done         bool       `json:"done"`
	IsDeleted    bool       `json:"is_deleted"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
}

// User represents an application user
type User struct {
	ID              int        `json:"id"`
	Email           string     `json:"email"`
	Name            string     `json:"name"`
	SlackID         string     `json:"slack_id"`
	WAJID           string     `json:"wa_jid"`
	Picture         string     `json:"picture"`
	Aliases         []string   `json:"aliases"`
	Points          int        `json:"points"`
	Streak          int        `json:"streak"`
	Level           int        `json:"level"`
	XP              int        `json:"xp"`
	DailyGoal       int        `json:"daily_goal"`
	LastCompletedAt *time.Time `json:"last_completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Achievement represents a gamification milestone
type Achievement struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Icon          string `json:"icon"`
	CriteriaType  string `json:"criteria_type"`
	CriteriaValue int    `json:"criteria_value"`
}

// UserAchievement joins users and achievements
type UserAchievement struct {
	UserID        int       `json:"user_id"`
	AchievementID int       `json:"achievement_id"`
	UnlockedAt    time.Time `json:"unlocked_at"`
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
	Task       string `json:"task"`
	Requester  string `json:"requester"`
	Assignee   string `json:"assignee"`
	AssignedAt string `json:"assigned_at"`
	SourceTS   string `json:"source_ts"`
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
