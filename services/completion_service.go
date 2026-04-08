package services

import (
	"context"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
)

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error)
	GetActiveContextTasks(ctx context.Context, email, source, room string) ([]store.ConsolidatedMessage, error)
	MarkMessageDone(ctx context.Context, email string, id int, done bool) error
	UpdateMessageCategory(ctx context.Context, email string, id int, category string) error
	HandleTaskState(ctx context.Context, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, email, threadID)
}

func (d *DefaultTaskStore) GetActiveContextTasks(ctx context.Context, email, source, room string) ([]store.ConsolidatedMessage, error) {
	return store.GetActiveContextTasks(ctx, email, source, room)
}

func (d *DefaultTaskStore) MarkMessageDone(ctx context.Context, email string, id int, done bool) error {
	return store.MarkMessageDone(ctx, email, id, done)
}

func (d *DefaultTaskStore) UpdateMessageCategory(ctx context.Context, email string, id int, category string) error {
	return store.UpdateMessageCategory(ctx, email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(ctx context.Context, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	return store.HandleTaskState(ctx, email, item, msg)
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
	threadTasks, _ := s.store.GetIncompleteByThreadID(ctx, msg.UserEmail, msg.ThreadID)
	roomTasks, _ := s.store.GetActiveContextTasks(ctx, msg.UserEmail, msg.Source, msg.Room)

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

	for _, res := range results {
		// Why: Handle 'new' tasks which don't have an ID yet.
		if res.State == "new" {
			if _, err := s.store.HandleTaskState(ctx, msg.UserEmail, res, msg); err != nil {
				logger.Errorf("[COMPLETION] Failed to handle new task: %v", err)
			}
			continue
		}

		if res.ID == nil || *res.ID == 0 {
			continue
		}
		// Why: routes the AI-determined state (resolve, update, cancel) to the database layer.
		// HandleTaskState ensures consistent state transitions and assignee updates.
		if _, err := s.store.HandleTaskState(ctx, msg.UserEmail, res, msg); err != nil {
			logger.Errorf("[COMPLETION] Failed to handle state for task %d: %v", *res.ID, err)
		}
	}
}

