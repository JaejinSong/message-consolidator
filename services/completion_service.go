package services

import (
	"context"
	"database/sql"
	"message-consolidator/ai"
	"message-consolidator/logger"
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
func (s *CompletionService) ProcessPotentialCompletion(ctx context.Context, msg store.ConsolidatedMessage) {
	if msg.ThreadID == "" && msg.RepliedToID == "" {
		return
	}
	targetID := msg.ThreadID
	if targetID == "" { targetID = msg.RepliedToID }

	// Why: Fetches thread-specific pending tasks to provide parent context for AI evaluation.
	tasks, _ := s.store.GetIncompleteByThreadID(ctx, s.db, msg.UserEmail, targetID)
	// Safety Check: If no incomplete tasks found, fallback instead of panicking on index 0.
	if len(tasks) == 0 {
		s.fallbackToNewExtraction(ctx, msg)
		return
	}

	// Why: [Thread-Aware Intelligence] Evaluates transition against the most relevant parent task.
	parent := tasks[0]
	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, parent.Task, msg.OriginalText)
	if err != nil {
		logger.Errorf("[COMPLETION] AI transition analysis failed: %v", err)
		return
	}

	s.handleCompletionResult(ctx, res.Status, res.UpdatedText, msg, parent)
}

func (s *CompletionService) handleCompletionResult(ctx context.Context, status, updatedText string, msg, parent store.ConsolidatedMessage) {
	switch status {
	case "RESOLVE":
		// Why: Pass s.db (*sql.DB) as a store.Querier. The store will handle internal transaction or direct execution.
		_ = s.store.MarkMessageDone(ctx, s.db, msg.UserEmail, parent.ID, true)
		logger.Infof("[COMPLETION] Task %d RESOLVED by reply %s", parent.ID, msg.SourceTS)
	case "UPDATE":
		if updatedText != "" {
			// Why: Consistently uses s.db for all store interactions to prevent global variable panics.
			_ = s.store.UpdateTaskText(ctx, s.db, msg.UserEmail, parent.ID, updatedText)
			logger.Infof("[COMPLETION] Task %d UPDATED by reply %s", parent.ID, msg.SourceTS)
		}
	case "NEW":
		s.fallbackToNewExtraction(ctx, msg)
	default:
		logger.Debugf("[COMPLETION] No state transition for reply %s", msg.SourceTS)
	}
}

func (s *CompletionService) fallbackToNewExtraction(ctx context.Context, msg store.ConsolidatedMessage) {
	enriched := types.EnrichedMessage{
		RawContent: msg.OriginalText, SourceChannel: msg.Source,
		SenderName: msg.Requester, VirtualThreadID: msg.ThreadID, Timestamp: msg.CreatedAt,
	}
	// Why: If AI identifies a 'NEW' task in a reply thread, we route it back to the standard extraction pipeline.
	items, err := s.gemini.Analyze(ctx, msg.UserEmail, enriched, "Korean", msg.Source, msg.Room)
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

