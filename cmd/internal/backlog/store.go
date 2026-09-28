// Package backlog holds the shared plumbing behind the reassess-*-backlog tools
// (cmd/reassess-slack-backlog, cmd/reassess-wa-backlog, cmd/reassess-gmail-backlog): a
// services.TaskStore wrapper that intercepts every write EvaluateThreadReply would
// otherwise apply directly so the tools can dry-run or downgrade every verdict to a
// confirm-first candidate, the Bootstrap init sequence and Finish footer every tool
// shares, plus the small display/config helpers all three tools share.
package backlog

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
)

// ProviderConfig builds the ai.ProviderConfig both reassess tools read off cfg.
func ProviderConfig(cfg *config.Config) ai.ProviderConfig {
	return ai.ProviderConfig{
		Provider:                 cfg.AIProvider,
		GeminiAPIKey:             cfg.GeminiAPIKey,
		GeminiAnalysisModel:      cfg.GeminiAnalysisModel,
		GeminiTranslationModel:   cfg.GeminiTranslationModel,
		DeepSeekAPIKey:           cfg.DeepSeekAPIKey,
		DeepSeekBaseURL:          cfg.DeepSeekBaseURL,
		DeepSeekFilterModel:      cfg.DeepSeekFilterModel,
		DeepSeekAnalysisModel:    cfg.DeepSeekAnalysisModel,
		DeepSeekTranslationModel: cfg.DeepSeekTranslationModel,
		DeepSeekReportModel:      cfg.DeepSeekReportModel,
	}
}

// Truncate collapses newlines and clips s to at most max runes, for one-line audit output.
func Truncate(s string, max int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max])
}

// ResultRow is one printed audit line: one (task, reply) pair the evaluator judged.
type ResultRow struct {
	TaskID  store.MessageID
	Room    string
	Verdict string
	Speaker string
	When    time.Time
	Quote   string
}

// Store wraps the real DefaultTaskStore so EvaluateThreadReply's writes never hard-close
// or auto-update a task from a reassess tool: every actionable verdict is recorded for
// the audit table and, only under Apply, persisted as a confirm-first candidate (never
// the real resolve/update).
//
// CurrentTask/CurrentSender/CurrentReplyTS must be set by the caller before each
// EvaluateThreadReply call so the write interception can check the task's own dismissal
// metadata and record the actual reply speaker and timestamp for the audit table.
type Store struct {
	Inner  services.TaskStore
	DB     *sql.DB
	Apply  bool
	Source string // e.g. "slack" or "whatsapp" -- used in the recorded Evidence string

	CurrentTask    store.ConsolidatedMessage
	CurrentSender  string
	CurrentReplyTS int64

	mu       sync.Mutex
	rows     []ResultRow
	resolved map[store.MessageID]bool
	written  int
}

// NewStore constructs a Store wrapping inner, gated on apply, labeling audit Evidence
// with source (e.g. "slack", "whatsapp").
func NewStore(inner services.TaskStore, db *sql.DB, apply bool, source string) *Store {
	return &Store{Inner: inner, DB: db, Apply: apply, Source: source}
}

func (s *Store) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return s.Inner.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (s *Store) HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error) {
	return s.Inner.HasAnyTaskInThread(ctx, q, email, threadID)
}

func (s *Store) GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error) {
	return s.Inner.GetLatestThreadAssignee(ctx, q, email, threadID)
}

func (s *Store) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error {
	return s.Inner.UpdateMessageCategory(ctx, q, email, id, category)
}

// UpdateSubtasks is a no-op: reassess tools never apply subtask cascades, only record
// confirm-first candidates.
func (s *Store) UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error {
	return nil
}

func (s *Store) GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error) {
	return s.Inner.GetRecentIncompleteGmail(ctx, q, email)
}

func (s *Store) SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]store.ConsolidatedMessage, error) {
	return s.Inner.SearchOpenTasksFTS(ctx, email, tokens, limit)
}

// HandleTaskState intercepts every RESOLVE (from the task's own assignee) and UPDATE
// verdict EvaluateThreadReply would otherwise apply directly. It never performs the real
// resolve/update; under Apply it downgrades the verdict to a confirm-first candidate
// instead, respecting any prior dismissal of the same source.
func (s *Store) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	verdict := strings.ToUpper(item.State)
	var id store.MessageID
	if item.ID != nil {
		id = *item.ID
	}
	// Why: CurrentSender is the reply's actual speaker; msg.Requester is a fallback for
	// callers that never set it -- both tools set CurrentSender before every call.
	speaker := s.CurrentSender
	if speaker == "" {
		speaker = msg.Requester
	}
	s.record(id, msg.Room, verdict+" (downgraded to candidate)", speaker, msg.OriginalText)
	if !s.Apply || item.ID == nil {
		return 0, nil
	}
	sourceKey := msg.Link
	if sourceKey == "" {
		sourceKey = msg.SourceTS
	}
	if store.WasCandidateDismissed(string(s.CurrentTask.Metadata), sourceKey) {
		return 0, nil
	}
	cand := store.CompletionCandidate{
		SourceLink: sourceKey,
		SourceText: Truncate(msg.OriginalText, 280),
		Evidence:   fmt.Sprintf("%s backlog reassessment (%s)", s.Source, verdict),
		DetectedAt: time.Now().UTC().Format(time.RFC3339),
		Status:     "pending",
	}
	if err := store.AddCompletionCandidate(ctx, s.DB, email, *item.ID, cand); err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.written++
	s.mu.Unlock()
	return *item.ID, nil
}

// AddCompletionCandidate is EvaluateThreadReply's own confirm-first path (RESOLVE from
// someone other than the assignee) -- the dismissal check already ran in the caller, so
// this only needs to gate the real write on Apply. speaker comes from CurrentSender: the
// store.TaskStore interface's AddCompletionCandidate does not carry the reply's sender,
// so it cannot be read off cand -- that omission left the audit row's speaker column
// hardcoded blank for every RESOLVE-from-counterparty verdict.
func (s *Store) AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error {
	s.record(id, s.CurrentTask.Room, "RESOLVE (candidate)", s.CurrentSender, cand.SourceText)
	if !s.Apply {
		return nil
	}
	if err := s.Inner.AddCompletionCandidate(ctx, q, email, id, cand); err != nil {
		return err
	}
	s.mu.Lock()
	s.written++
	s.mu.Unlock()
	return nil
}

// IsResolved reports whether any recorded row for id carries a RESOLVE verdict -- used to
// stop evaluating further candidate replies for a task once one resolves it.
func (s *Store) IsResolved(id store.MessageID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolved[id]
}

// Written returns the number of confirm-first candidates actually persisted so far.
func (s *Store) Written() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.written
}

// Rows returns a snapshot of the recorded audit rows.
func (s *Store) Rows() []ResultRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ResultRow(nil), s.rows...)
}

func (s *Store) record(id store.MessageID, room, verdict, speaker, quote string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	when := time.Now().UTC()
	if s.CurrentReplyTS > 0 {
		when = time.Unix(s.CurrentReplyTS, 0).UTC()
	}
	s.rows = append(s.rows, ResultRow{
		TaskID: id, Room: room, Verdict: verdict, Speaker: speaker,
		When: when, Quote: Truncate(quote, 80),
	})
	if strings.HasPrefix(verdict, "RESOLVE") {
		if s.resolved == nil {
			s.resolved = map[store.MessageID]bool{}
		}
		s.resolved[id] = true
	}
}

// PrintResults prints every recorded audit row, sorted by task ID, including the
// evaluated reply's timestamp (When falls back to the time of evaluation when the caller
// never set CurrentReplyTS).
func (s *Store) PrintResults() {
	rows := s.Rows()
	sort.Slice(rows, func(i, j int) bool { return rows[i].TaskID < rows[j].TaskID })
	fmt.Printf("\n%-8s %-20s %-32s %-20s %-12s %s\n", "id", "room", "verdict", "speaker", "when", "quote")
	for _, r := range rows {
		fmt.Printf("%-8d %-20s %-32s %-20s %-12s %s\n", r.TaskID, r.Room, r.Verdict, r.Speaker, r.When.Format("2006-01-02"), r.Quote)
	}
	if len(rows) == 0 {
		fmt.Println("(no actionable verdicts found)")
	}
}
