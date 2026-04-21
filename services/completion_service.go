package services

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/store"
	"message-consolidator/types"
)

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
	EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string) (ai.TaskTransition, error)
	Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error)
	GetActiveContextTasks(ctx context.Context, q store.Querier, email, source, room string) ([]store.ConsolidatedMessage, error)
	MarkMessageDone(ctx context.Context, q store.Querier, email string, id int, done bool) error
	UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id int, category string) error
	HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error)
	UpdateTaskText(ctx context.Context, q store.Querier, email string, id int, task string) error
	GetMessageByID(ctx context.Context, q store.Querier, email string, id int) (store.ConsolidatedMessage, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) GetActiveContextTasks(ctx context.Context, q store.Querier, email, source, room string) ([]store.ConsolidatedMessage, error) {
	return store.GetActiveContextTasks(ctx, q, email, source, room)
}

func (d *DefaultTaskStore) MarkMessageDone(ctx context.Context, q store.Querier, email string, id int, done bool) error {
	return store.MarkMessageDone(ctx, q, email, id, done)
}

func (d *DefaultTaskStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id int, category string) error {
	return store.UpdateMessageCategory(ctx, q, email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	return store.HandleTaskState(ctx, q, email, item, msg)
}

func (d *DefaultTaskStore) UpdateTaskText(ctx context.Context, q store.Querier, email string, id int, task string) error {
	return store.UpdateTaskText(ctx, q, email, id, task)
}

func (d *DefaultTaskStore) GetMessageByID(ctx context.Context, q store.Querier, email string, id int) (store.ConsolidatedMessage, error) {
	return store.GetMessageByID(ctx, q, email, id)
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
		s.fallbackToNewExtraction(ctx, msg)
		return false, nil // While extracted as NEW, it wasn't a transition.
	}

	parent := tasks[0]
	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, parent.Task, msg.OriginalText)
	if err != nil {
		return false, fmt.Errorf("transition analysis failed: %w", err)
	}

	return s.handleCompletionResult(ctx, res.Status, res.UpdatedText, msg, parent), nil
}

func (s *CompletionService) handleCompletionResult(ctx context.Context, status, updatedText string, msg, parent store.ConsolidatedMessage) bool {
	switch status {
	case "RESOLVE":
		_ = s.store.MarkMessageDone(ctx, s.db, msg.UserEmail, parent.ID, true)
		return true
	case "UPDATE":
		if updatedText != "" {
			_ = s.store.UpdateTaskText(ctx, s.db, msg.UserEmail, parent.ID, updatedText)
			return true
		}
	case "NEW":
		s.fallbackToNewExtraction(ctx, msg)
	}
	return false
}

func (s *CompletionService) fallbackToNewExtraction(ctx context.Context, msg store.ConsolidatedMessage) {
	enriched := types.EnrichedMessage{
		RawContent: msg.OriginalText, SourceChannel: msg.Source,
		SenderName: msg.Requester, VirtualThreadID: msg.ThreadID, Timestamp: msg.CreatedAt,
	}
	// Why: If AI identifies a 'NEW' task in a reply thread, we route it back to the standard extraction pipeline.
	room := msg.Room
	if room == "" {
		room = "General"
	}
	items, err := s.gemini.Analyze(ctx, msg.UserEmail, enriched, "Korean", msg.Source, room)
	if err != nil || len(items) == 0 { return }

	_ = s.runInTx(ctx, func(tx *sql.Tx) error {
		for _, item := range items {
			_, _ = s.store.HandleTaskState(ctx, tx, msg.UserEmail, item, msg)
		}
		return nil
	})
}

func (s *CompletionService) runInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil { return err }
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

