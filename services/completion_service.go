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

	// Retrieve all incomplete tasks associated with this thread to determine if the new message resolves any of them.
	tasks, err := s.store.GetIncompleteByThreadID(ctx, msg.UserEmail, msg.ThreadID)
	if err != nil {

		logger.Errorf("[COMPLETION] Failed to fetch incomplete tasks for thread %s: %v", msg.ThreadID, err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	// Defense mechanism: A message containing an '@' mention typically indicates delegation rather than task completion.
	// We bypass the auto-completion logic here to prevent prematurely closing a task that is merely being handed off,
	// leaving the mention handling to the dedicated scanner logic.
	if strings.Contains(msg.OriginalText, "@") {
		logger.Infof("[COMPLETION] Skip auto-completion for thread %s (reply %s): Message contains mention", msg.ThreadID, msg.SourceTS)
		return
	}

	logger.Infof("[COMPLETION] Found %d incomplete tasks in thread %s.", len(tasks), msg.ThreadID)

	// Optimization: For threads with numerous tasks (>= 3), process them in a single batch request to the AI model.
	// This significantly reduces token consumption and API overhead compared to individual checks.
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

	// Fallback: For threads with only 1 or 2 tasks, process them individually as the batching overhead is unnecessary.
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
