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
	UpdateMessageCategory(email string, id int, category string) error
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

	//Why: Retrieves all incomplete tasks associated with this thread to determine if the new message resolves any of them.
	tasks, err := s.store.GetIncompleteByThreadID(ctx, msg.UserEmail, msg.ThreadID)
	if err != nil {

		logger.Errorf("[COMPLETION] Failed to fetch incomplete tasks for thread %s: %v", msg.ThreadID, err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	//Why: Extracts the actual reply content, excluding email headers, to ensure the AI's analysis is focused on the message body and not metadata.
	textToAnalyze := msg.OriginalText
	mentionCheckText := msg.OriginalText

	if msg.Source == "gmail" {
		//Why: Gmail messages use a specific header format (T:, C:, S:, B:). We extract only the content after 'B:' to isolate the body from email addresses in headers.
		if parts := strings.SplitN(msg.OriginalText, "\nB:\n", 2); len(parts) == 2 {
			textToAnalyze = parts[1]
			mentionCheckText = parts[1]
		}
	}

	//Why: Prevents prematurely closing tasks that are being delegated by excluding messages containing '@' mentions in the actual message body.
	if strings.Contains(mentionCheckText, "@") {
		logger.Infof("[COMPLETION] Skip auto-completion for thread %s (reply %s): Message contains mention in body", msg.ThreadID, msg.SourceTS)
		return
	}

	//Why: Implements a 'Two-Phase' status transition where any reply immediately releases a 'Waiting for Reply' state to 'Others', even before AI completion analysis.
	for _, task := range tasks {
		if task.Category == "waiting" {
			logger.Infof("[COMPLETION] Auto-releasing 'waiting' status for task %d in thread %s", task.ID, msg.ThreadID)
			if err := s.store.UpdateMessageCategory(msg.UserEmail, task.ID, "others"); err != nil {
				logger.Errorf("[COMPLETION] Failed to release waiting status for task %d: %v", task.ID, err)
			}
		}
	}

	logger.Infof("[COMPLETION] Found %d incomplete tasks in thread %s.", len(tasks), msg.ThreadID)

	//Why: Reduces token consumption and API overhead by processing threads with 3 or more tasks in a single batch request to the AI model.
	if len(tasks) >= 3 {
		logger.Infof("[COMPLETION] Using batch analysis for %d tasks in thread %s", len(tasks), msg.ThreadID)
		completedIDs, err := s.gemini.CheckTasksBatch(ctx, msg.UserEmail, textToAnalyze, tasks)
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

	//Why: Processes small threads individually to avoid the unnecessary overhead of batching logic when only 1 or 2 tasks are involved.
	for _, task := range tasks {
		//Why: Ensures a message does not accidentally mark itself as completed by skipping the comparison if the source timestamps match.
		if task.SourceTS == msg.SourceTS {
			continue
		}

		isDone, err := s.gemini.DoesReplyCompleteTask(ctx, msg.UserEmail, task.OriginalText, textToAnalyze)
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
