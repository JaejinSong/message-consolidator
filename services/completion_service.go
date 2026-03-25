package services

import (
	"context"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

type AICompleter interface {
	DoesReplyCompleteTask(ctx context.Context, email, taskText, replyText string) (bool, error)
	CheckTasksBatch(ctx context.Context, email, replyText string, tasks []store.ConsolidatedMessage) ([]int, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error)
	MarkMessageDone(email string, id int, done bool) error
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, email, threadID)
}

func (d *DefaultTaskStore) MarkMessageDone(email string, id int, done bool) error {
	return store.MarkMessageDone(email, id, done)
}

type CompletionService struct {
	gemini AICompleter
	store  TaskStore
}

func NewCompletionService(gemini AICompleter, taskStore TaskStore) *CompletionService {
	return &CompletionService{gemini: gemini, store: taskStore}
}




// ProcessPotentialCompletion checks if a new message (reply) completes any existing tasks in the same thread.
func (s *CompletionService) ProcessPotentialCompletion(ctx context.Context, msg store.ConsolidatedMessage) {
	if msg.ThreadID == "" {
		return
	}

	// 1. Find all incomplete tasks in this thread for this user
	tasks, err := s.store.GetIncompleteByThreadID(ctx, msg.UserEmail, msg.ThreadID)
	if err != nil {

		logger.Errorf("[COMPLETION] Failed to fetch incomplete tasks for thread %s: %v", msg.ThreadID, err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	// 2. Defense: If the message contains a mention (@colleague), it's likely a delegation or asking for help.
	// The scanner_slack.go's classifyMessage logic handles mentions by creating "Waiting On" tasks.
	// We skip auto-completion here to avoid marking the task as done when it's just being pushed to someone else.
	if strings.Contains(msg.OriginalText, "@") {
		logger.Infof("[COMPLETION] Skip auto-completion for thread %s (reply %s): Message contains mention", msg.ThreadID, msg.SourceTS)
		return
	}

	logger.Infof("[COMPLETION] Found %d incomplete tasks in thread %s.", len(tasks), msg.ThreadID)

	// 4. Optimization: If many tasks, use batch analysis to save tokens
	if len(tasks) >= 3 {
		logger.Infof("[COMPLETION] Using batch analysis for %d tasks in thread %s", len(tasks), msg.ThreadID)
		completedIDs, err := s.gemini.CheckTasksBatch(ctx, msg.UserEmail, msg.OriginalText, tasks)
		if err != nil {
			logger.Errorf("[COMPLETION] Batch check failed: %v", err)
			return
		}

		for _, id := range completedIDs {
			logger.Infof("[COMPLETION] Task %d marked as DONE by batch reply %s", id, msg.SourceTS)
			if err := s.store.MarkMessageDone(msg.UserEmail, id, true); err != nil {
				logger.Errorf("[COMPLETION] Failed to mark task %d as done: %v", id, err)
			}
		}
		return
	}

	// 5. Individual check for fewer tasks
	for _, task := range tasks {
		// Prevent self-completion
		if task.SourceTS == msg.SourceTS {
			continue
		}

		isDone, err := s.gemini.DoesReplyCompleteTask(ctx, msg.UserEmail, task.OriginalText, msg.OriginalText)
		if err != nil {
			logger.Errorf("[COMPLETION] Error checking completion for task %d: %v", task.ID, err)
			continue
		}

		if isDone {
			logger.Infof("[COMPLETION] Task %d marked as DONE by reply %s", task.ID, msg.SourceTS)
			if err := s.store.MarkMessageDone(msg.UserEmail, task.ID, true); err != nil {
				logger.Errorf("[COMPLETION] Failed to mark task %d as done: %v", task.ID, err)
			}
		}
	}
}

