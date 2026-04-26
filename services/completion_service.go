package services

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
)

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
	EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string) (ai.TaskTransition, error)
	Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error)
	UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error
	HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error {
	return store.UpdateMessageCategory(ctx, q, email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	return HandleTaskState(ctx, q, email, item, msg)
}

type CompletionService struct {
	gemini   AICompleter
	store    TaskStore
	tasksSvc *TasksService
	db       *sql.DB
}

func NewCompletionService(gemini AICompleter, taskStore TaskStore, tasksSvc *TasksService, db *sql.DB) *CompletionService {
	return &CompletionService{gemini: gemini, store: taskStore, tasksSvc: tasksSvc, db: db}
}

// ProcessPotentialCompletion checks if a message (reply) completes/updates tasks in the same thread.
// Why: [Early Return] Returns true if the message was handled as a task completion/update, signaling the scanner to skip extraction.
func (s *CompletionService) ProcessPotentialCompletion(ctx context.Context, msg store.ConsolidatedMessage) (bool, error) {
	if msg.ThreadID == "" && msg.RepliedToID == "" {
		return false, nil
	}
	targetID := msg.ThreadID
	if targetID == "" { targetID = msg.RepliedToID }

	tasks, _ := s.store.GetIncompleteByThreadID(ctx, s.db, msg.UserEmail, targetID)
	if len(tasks) == 0 {
		// Why: Fallback consumes its own AI Analyze + persists tasks. Returning true
		// signals the caller to MarkAsProcessed so the next scan cycle skips this msg
		// instead of paying for LiteFilter + Analyze + batch Analyze again.
		return s.fallbackToNewExtraction(ctx, msg), nil
	}

	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, tasks[0].Task, msg.OriginalText)
	if err != nil {
		return false, fmt.Errorf("transition analysis failed: %w", err)
	}

	// Why: Apply the same transition to all incomplete tasks in the thread —
	// a single reply affects every open item from that conversation.
	handled := false
	for _, task := range tasks {
		if s.handleCompletionResult(ctx, res.Status, res.UpdatedText, msg, task) {
			handled = true
		}
	}
	return handled, nil
}

func (s *CompletionService) handleCompletionResult(ctx context.Context, status, updatedText string, msg, parent store.ConsolidatedMessage) bool {
	fromMe := strings.EqualFold(msg.RequesterCanonical, msg.UserEmail)
	parentID := parent.ID
	switch status {
	case "RESOLVE":
		item := store.TodoItem{State: "resolve", ID: &parentID}
		_, _ = s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
		return true
	case "UPDATE":
		// Why: If the current user sent the reply and the task isn't fully resolved,
		// it moves to 맡긴 업무 — the ball is in the other party's court.
		if fromMe {
			_ = s.store.UpdateMessageCategory(ctx, s.db, msg.UserEmail, parent.ID, CategoryRequested)
		}
		if updatedText != "" {
			item := store.TodoItem{State: "update", ID: &parentID, Task: updatedText}
			_, _ = s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
		}
		if fromMe || updatedText != "" {
			return true
		}
	case "NEW":
		return s.fallbackToNewExtraction(ctx, msg)
	}
	return false
}

// fallbackToNewExtraction runs an isolated AI extraction for messages whose thread
// has no incomplete parent task. Returns true once AI Analyze succeeds so callers
// can MarkAsProcessed and avoid paying tokens again next scan cycle (the prior
// void return left the message in filteredMsgs, causing a second batch Analyze
// in processBatch within the same cycle and re-extraction every cycle thereafter).
func (s *CompletionService) fallbackToNewExtraction(ctx context.Context, msg store.ConsolidatedMessage) bool {
	enriched := types.EnrichedMessage{
		RawContent: msg.OriginalText, SourceChannel: msg.Source,
		SenderName: msg.Requester, VirtualThreadID: msg.ThreadID, Timestamp: msg.CreatedAt,
	}
	room := msg.Room
	if room == "" {
		room = "General"
	}
	items, err := s.gemini.Analyze(ctx, msg.UserEmail, enriched, "Korean", msg.Source, room)
	if err != nil || len(items) == 0 {
		return false
	}

	// Why: items are independent SaveMessage calls — wrapping them in a single
	// outer tx only widened the libsql writer-lock window and silently swallowed
	// per-item INSERT failures via tx.Commit() on a nil-return fn. WithDBRetry
	// absorbs transient `database is locked` errors (5 attempts, 100ms→1.6s
	// backoff). AI cost is sunk regardless of save outcome — the bool return is
	// what stops the token bleed, not save success.
	for _, item := range items {
		err := store.WithDBRetry("CompletionFallback.HandleTaskState", func() error {
			_, e := s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
			return e
		})
		if err != nil {
			logger.Warnf("[COMPLETION-FALLBACK] HandleTaskState dropped item after retries: %v", err)
		}
	}
	return true
}

