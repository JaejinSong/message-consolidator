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
	Pinned       bool       `json:"pinned"`
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
	ConsolidatedContext []string       `json:"consolidated_context"` //Why: Stores 1-2 sentence snippets from the original message that justify the task.
	Metadata           json.RawMessage `json:"metadata"`         //Why: Flexible JSON storage for future-proofing message attributes.
	SourceChannels     []string        `json:"source_channels"`  //Why: Tracks all channels that contributed to this consolidated task.
	RequesterType      string          `json:"requester_type,omitempty"`
	AssigneeType       string          `json:"assignee_type,omitempty"`
	RequesterDisplayName string          `json:"requester_display_name,omitempty"`
	AssigneeDisplayName  string          `json:"assignee_display_name,omitempty"`
	Subtasks             []Subtask      `json:"subtasks,omitempty"` //Why: Hierarchical task structure for consolidated emails.
}

// Subtask represents a smaller action item within a consolidated task.
type Subtask struct {
	Task       string `json:"task"`
	AssigneeID *int   `json:"assignee_id"`
	Assignee   string `json:"assignee"`
	Done       bool   `json:"done"`
}

// CategorizedMessages represents groups of messages for the dashboard tabs.
// Why: Enables Zero-Logic UI by delegating all filtering and categorization to the backend.
type CategorizedMessages struct {
	Inbox   []ConsolidatedMessage `json:"inbox" Gennode:"true"`   // Active tasks assigned to the user ('me' or userName)
	Pending []ConsolidatedMessage `json:"pending" Gennode:"true"` // Active tasks assigned to others
	All     []ConsolidatedMessage `json:"all" Gennode:"true"`     // All active tasks regardless of assignee or category
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
	ArchiveDays     int        `json:"archive_days"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (u User) PreferredName() string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}


// TaskTranslation represents a cached translation for a task
type TaskTranslation struct {
	MessageID      int    `json:"message_id"`
	LanguageCode   string `json:"language_code"`
	TranslatedText string `json:"translated_text"`
}

// UserAlias represents a name alias for a user
const (
	// Identifier types (how we resolve them)
	ContactTypeEmail    = "email"
	ContactTypeName     = "name"
	ContactTypeWhatsApp = "whatsapp"
	ContactTypeSlack    = "slack"

	// Contact categories (who they are / categorization)
	CategoryInternal = "internal"
	CategoryPartner  = "partner"
	CategoryCustomer = "customer"
	CategoryNone     = "none"

	// Source identifiers (where the data came from)
	SourceSlack    = "slack"
	SourceWhatsApp = "whatsapp"
	SourceGmail    = "gmail"
	SourceManual   = "manual"
	SourceAll      = "all"
)

type UserAlias struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	AliasName string `json:"alias_name"`
}

// TodoItem is the task structure returned by Gemini Analyze
type TodoItem struct {
	ID              *int            `json:"id,omitempty"` // ID of the existing task to update or resolve
	State           string          `json:"state"`        // "new", "update", "resolve", or "cancel"
	Reasoning       string          `json:"reasoning,omitempty"` // AI justification for state/merge choice
	Task               string          `json:"task"`
	Requester          string          `json:"requester"`
	RequesterCanonical string          `json:"requester_canonical,omitempty"`
	Assignee           string          `json:"assignee"`
	AssignedTo      string          `json:"assigned_to,omitempty"`
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
	SourceChannels  []string        `json:"source_channels,omitempty"`  // All origins for merged tasks.
	ContextSnippets []string        `json:"context_snippets,omitempty"` // Justification snippets for the task.
	Status          string          `json:"status"`            // "new", "update", "resolve", or "cancel"
	Subtasks        []TodoSubtask   `json:"subtasks,omitempty"`        // Why: Support for hierarchical task-subtask structure from AI.
}

// TodoSubtask represents a sub-action extracted by AI.
type TodoSubtask struct {
	Task         string `json:"task"`
	AssigneeID   *int   `json:"assignee_id"`
	AssigneeName string `json:"assignee_name"`
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
	DailyCompletions        map[string]int    `json:"daily_completions"`
	HourlyActivity          map[int]int       `json:"hourly_activity"`
	PeakTime                string            `json:"peak_time"`
	AbandonedTasks          int               `json:"abandoned_tasks"`
	PendingMe               int               `json:"pending_me"`
	PendingOthers           int               `json:"pending_others"`            //Why: Number of incomplete tasks assigned to others to support tab counts in the main UI.
	SourceDistribution      map[string]int    `json:"source_distribution"`       //Why: Represents the distribution of active tasks for the dashboard.
	SourceDistributionTotal map[string]int    `json:"source_distribution_total"` //Why: Represents the total distribution including archived tasks.
	CompletionHistory       []TimeSeriesPoint `json:"completion_history"`
	InternalTaskCount       int               `json:"internal_task_count"`
	PartnerTaskCount        int               `json:"partner_task_count"`
	CustomerTaskCount       int               `json:"customer_task_count"`
	ExternalTaskCount       int               `json:"external_task_count"`
}

// ReportTranslation represents a specific language version of an AI-generated report summary.
type ReportTranslation struct {
	LanguageCode string `json:"language_code"`
	Summary      string `json:"summary"`
}

const (
	ReportStatusProcessing = "processing"
	ReportStatusCompleted  = "completed"
	ReportStatusFailed     = "failed"
)

// Report represents a cached AI-generated summary (metadata) and backend-calculated visualization.
// Why: Standardizes the 1:N relationship where one metadata entry can have multiple language translations.
type Report struct {
	ID            int                 `json:"id"`
	UserEmail     string              `json:"user_email"`
	StartDate     string              `json:"start_date"`
	EndDate       string              `json:"end_date"`
	ReportSummary string              `json:"report_summary"` // Primary summary (typically English)
	Translations  map[string]string   `json:"translations,omitempty"`
	Visualization string              `json:"visualization_data"` // JSON string of Node/Edge data
	Status        string              `json:"status"`             // "processing", "completed", "failed"
	IsTruncated   bool                `json:"is_truncated"`       // Why: Flag to indicate if the report was limited due to token boundaries.
	CreatedAt     time.Time           `json:"created_at"`
}

// AliasStore provides an interface for metadata enrichment and identity resolution.
// Why: Standardizes the enrichment pipeline by providing a dedicated handle for store-layer lookups.
type AliasStore struct{}
