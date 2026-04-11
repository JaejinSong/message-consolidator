package services

import (
	"context"
	"database/sql"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error)
	GetActiveContextTasks(ctx context.Context, q store.Querier, email, source, room string) ([]store.ConsolidatedMessage, error)
	MarkMessageDone(ctx context.Context, tx *sql.Tx, email string, id int, done bool) error
	UpdateMessageCategory(ctx context.Context, tx *sql.Tx, email string, id int, category string) error
	HandleTaskState(ctx context.Context, tx *sql.Tx, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) GetActiveContextTasks(ctx context.Context, q store.Querier, email, source, room string) ([]store.ConsolidatedMessage, error) {
	return store.GetActiveContextTasks(ctx, q, email, source, room)
}

func (d *DefaultTaskStore) MarkMessageDone(ctx context.Context, tx *sql.Tx, email string, id int, done bool) error {
	return store.MarkMessageDone(ctx, tx, email, id, done)
}

func (d *DefaultTaskStore) UpdateMessageCategory(ctx context.Context, tx *sql.Tx, email string, id int, category string) error {
	return store.UpdateMessageCategory(ctx, tx, email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(ctx context.Context, tx *sql.Tx, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	return store.HandleTaskState(ctx, tx, email, item, msg)
}

type CompletionService struct {
	gemini AICompleter
	store  TaskStore
}

func NewCompletionService(gemini AICompleter, taskStore TaskStore) *CompletionService {
	return &CompletionService{gemini: gemini, store: taskStore}
}

// ProcessPotentialCompletion checks if a message (reply) completes/updates tasks in the same thread.
func (s *CompletionService) ProcessPotentialCompletion(ctx context.Context, msg store.ConsolidatedMessage) {
	if msg.ThreadID == "" {
		return
	}

	// Why: Fetches both thread-specific and room-wide pending tasks to provide full context for AI state determination.
	threadTasks, _ := s.store.GetIncompleteByThreadID(ctx, store.GetDB(), msg.UserEmail, msg.ThreadID)
	roomTasks, _ := s.store.GetActiveContextTasks(ctx, store.GetDB(), msg.UserEmail, msg.Source, msg.Room)

	// Merge tasks, avoiding duplicates (prioritizing thread relevance)
	taskMap := make(map[int]store.ConsolidatedMessage)
	for _, t := range roomTasks {
		taskMap[t.ID] = t
	}
	for _, t := range threadTasks {
		taskMap[t.ID] = t
	}

	var tasks []store.ConsolidatedMessage
	for _, t := range taskMap {
		tasks = append(tasks, t)
	}

	// from conversational context. 'allTasks' will just be empty.

	// Why: Leverages the unified AI analysis pipeline to determine if the reply resolves or delegates tasks.
	// Mentions and intentionality are parsed by the AI prompt, not string matching.
	enriched := types.EnrichedMessage{
		RawContent:      msg.OriginalText,
		SourceChannel:   msg.Source,
		SenderID:        0, // ID is not directly available in ConsolidatedMessage, using 0 as fallback.
		SenderName:      msg.Requester,
		VirtualThreadID: msg.ThreadID,
		Timestamp:       msg.CreatedAt,
	}
	results, err := s.gemini.AnalyzeWithContext(ctx, msg.UserEmail, enriched, "Korean", msg.Source, msg.Room, tasks)
	if err != nil {
		logger.Errorf("[COMPLETION] AI analysis failed for thread %s: %v", msg.ThreadID, err)
		return
	}

	// Why: [Transaction] Use RunInTx to ensure atomicity for multi-item results from AI.
	_ = store.RunInTx(ctx, func(tx *sql.Tx) error {
		for _, res := range results {
			// Why: Mandatory Room Isolation Check. 
			// If AI hallucinated an ID from another room, we drop only that item (Partial Success).
			if res.ID != nil && *res.ID != 0 {
				existing, _ := store.GetMessageByID(ctx, tx, msg.UserEmail, int(*res.ID))
				if existing.Room != msg.Room {
					logger.Errorf("[SECURITY] ID Cross-room update attempted by AI: ID %d (Room: %s) vs Message Room: %s. Dropping item.", *res.ID, existing.Room, msg.Room)
					continue
				}
			}

			if _, err := s.store.HandleTaskState(ctx, tx, msg.UserEmail, res, msg); err != nil {
				logger.Errorf("[COMPLETION] State update failed: %v", err)
				// Note: Returning error here would rollback the WHOLE transaction.
				// Since AI responses can sometimes contain one bad item, we log it and continue if possible, 
				// or return nil to commit the successful ones.
			}
		}
		return nil
	})
}

