package store

import (
	"encoding/json"
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
	Time           time.Time //Why: Maintains compatibility with WhatsApp's time field naming convention.
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
	AssignedAt   time.Time  `json:"assigned_at"`
	Link         string     `json:"link"`
	SourceTS     string     `json:"source_ts"`
	OriginalText string     `json:"original_text,omitempty"`
	HasOriginal  bool       `json:"has_original,omitempty"`
	Done         bool       `json:"done"`
	IsDeleted    bool       `json:"is_deleted"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	Category     string     `json:"category"`
	Deadline     string     `json:"deadline,omitempty"`
	ThreadID           string     `json:"thread_id,omitempty"`
	RequesterCanonical string     `json:"requester_canonical,omitempty"`
	AssigneeCanonical  string     `json:"assignee_canonical,omitempty"`
	AssigneeReason     string     `json:"assignee_reason,omitempty"`
	RepliedToID        string     `json:"replied_to_id,omitempty"`
	IsContextQuery     bool            `json:"is_context_query"` //Why: Indicates if the message is a follow-up inquiry about existing policies or tasks.
	Constraints        []string        `json:"constraints"`      //Why: Stores behavioral rules extracted from POLICY-type messages.
	Metadata           json.RawMessage `json:"metadata"`         //Why: Flexible JSON storage for future-proofing message attributes.
}

// CategorizedMessages represents groups of messages for the dashboard tabs.
// Why: Enables Zero-Logic UI by delegating all filtering and categorization to the backend.
type CategorizedMessages struct {
	Inbox   []ConsolidatedMessage `json:"inbox"`   // Active tasks assigned to the user ('me' or userName)
	Pending []ConsolidatedMessage `json:"pending"` // Active tasks assigned to others
	Waiting []ConsolidatedMessage `json:"waiting"` // Active tasks in 'waiting' category
	All     []ConsolidatedMessage `json:"all"`     // All active tasks regardless of assignee or category
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
	StreakFreezes   int        `json:"streak_freezes"`
	ArchiveDays     int        `json:"archive_days"`
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
	TargetValue   int    `json:"target_value"` //Why: Provides a compatibility alias for the frontend's expected target_value field.
	XPReward      int    `json:"xp_reward"`
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
	LanguageCode   string `json:"language_code"`
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
	ID              *int            `json:"id,omitempty"` // ID of the existing task to update or resolve
	State           string          `json:"state"`        // "new", "update", "resolve", or "cancel"
	Task            string          `json:"task"`
	Requester       string          `json:"requester"`
	Assignee        string          `json:"assignee"`
	AssignedAt      string          `json:"assigned_at"`
	SourceTS        string          `json:"source_ts"`
	Category        string          `json:"category"`
	Deadline        string          `json:"deadline"`
	AssigneeReason  string          `json:"assignee_reason,omitempty"`
	IsContextQuery  bool            `json:"is_context_query"`
	Constraints     []string        `json:"constraints"`
	Metadata        json.RawMessage `json:"metadata"`
	AffinityScore   int             `json:"affinity_score,omitempty"`   // Contextual similarity score (0-100) for consolidation.
	AffinityGroupID string          `json:"affinity_group_id,omitempty"` // Shared ID for tasks that should be consolidated.
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

// Why: Encapsulates query parameters to prevent long argument lists and improve maintainability.
type ArchiveFilter struct {
	Email  string
	Limit  int
	Offset int
	Query  string
	Sort   string
	Order  string
	Status string //Why: Supports status filters such as "all", "done", or "canceled".
}

// TimeSeriesPoint represents daily task completions grouped by source
type TimeSeriesPoint struct {
	Date   string         `json:"date"`
	Counts map[string]int `json:"counts"`
}

// UserStats represents various productivity metrics for a user
type UserStats struct {
	TotalCompleted          int               `json:"total_completed"`
	DailyGoal               int               `json:"daily_goal"`
	DailyCompletions        map[string]int    `json:"daily_completions"`
	HourlyActivity          map[int]int       `json:"hourly_activity"`
	PeakTime                string            `json:"peak_time"`
	AbandonedTasks          int               `json:"abandoned_tasks"`
	PendingMe               int               `json:"pending_me"`
	WaitingTasks            int               `json:"waiting_tasks"`             //Why: Number of incomplete tasks with 'waiting' category for the dashboard widget.
	PendingOthers           int               `json:"pending_others"`            //Why: Number of incomplete tasks assigned to others to support tab counts in the main UI.
	SourceDistribution      map[string]int    `json:"source_distribution"`       //Why: Represents the distribution of active tasks for the dashboard.
	SourceDistributionTotal map[string]int    `json:"source_distribution_total"` //Why: Represents the total distribution including archived tasks.
	CompletionHistory       []TimeSeriesPoint `json:"completion_history"`
}

// ReportTranslation represents a specific language version of an AI-generated report summary.
type ReportTranslation struct {
	LanguageCode string `json:"language_code"`
	Summary      string `json:"summary"`
}

// Report represents a cached AI-generated summary (metadata) and backend-calculated visualization.
// Why: Standardizes the 1:N relationship where one metadata entry can have multiple language translations.
type Report struct {
	ID            int                 `json:"id"`
	UserEmail     string              `json:"user_email"`
	StartDate     string              `json:"start_date"`
	EndDate       string              `json:"end_date"`
	Summary       string            `json:"report_summary"` // Primary summary (typically English)
	Translations  map[string]string `json:"translations,omitempty"`
	Visualization string              `json:"visualization_data"` // JSON string of Node/Edge data
	IsTruncated   bool                `json:"is_truncated"`       // Why: Flag to indicate if the report was limited due to token boundaries.
	CreatedAt     time.Time           `json:"created_at"`
}
