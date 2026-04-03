package services

import (
	"context"
	"message-consolidator/logger"
	"message-consolidator/store"
)

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email, conversationText, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error)
	MarkMessageDone(email string, id int, done bool) error
	UpdateMessageCategory(email string, id int, category string) error
	HandleTaskState(email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, email, threadID)
}

func (d *DefaultTaskStore) MarkMessageDone(email string, id int, done bool) error {
	return store.MarkMessageDone(email, id, done)
}

func (d *DefaultTaskStore) UpdateMessageCategory(email string, id int, category string) error {
	return store.UpdateMessageCategory(email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	return store.HandleTaskState(email, item, msg)
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

	tasks, err := s.store.GetIncompleteByThreadID(ctx, msg.UserEmail, msg.ThreadID)
	if err != nil || len(tasks) == 0 {
		return
	}

	s.releaseWaitingStatus(msg.UserEmail, tasks)

	// Why: Leverages the unified AI analysis pipeline to determine if the reply resolves or delegates tasks.
	// Mentions and intentionality are parsed by the AI prompt, not string matching.
	results, err := s.gemini.AnalyzeWithContext(ctx, msg.UserEmail, msg.OriginalText, "Korean", msg.Source, msg.Room, tasks)
	if err != nil {
		logger.Errorf("[COMPLETION] AI analysis failed for thread %s: %v", msg.ThreadID, err)
		return
	}

	for _, res := range results {
		if res.ID == nil || *res.ID == 0 {
			continue
		}
		// Why: routes the AI-determined state (resolve, update, cancel) to the database layer.
		// HandleTaskState ensures consistent state transitions and assignee updates.
		if _, err := s.store.HandleTaskState(msg.UserEmail, res, msg); err != nil {
			logger.Errorf("[COMPLETION] Failed to handle state for task %d: %v", *res.ID, err)
		}
	}
}

func (s *CompletionService) releaseWaitingStatus(email string, tasks []store.ConsolidatedMessage) {
	for _, task := range tasks {
		if task.Category == "waiting" {
			logger.Infof("[COMPLETION] Auto-releasing 'waiting' status for task %d", task.ID)
			s.store.UpdateMessageCategory(email, task.ID, "others")
		}
	}
}
