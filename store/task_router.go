package store

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"time"
)

// HandleTaskState routes task operations based on the AI-determined state.
// Why: Centralizes task state transitions to ensure consistency. Refactored into helpers to maintain 30-line limit.
func HandleTaskState(ctx context.Context, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	switch item.State {
	case "none":
		return handleNone()
	case "new":
		return handleNew(ctx, item, msg)
	case "update":
		return handleUpdate(ctx, email, item, msg)
	case "resolve":
		return handleResolve(ctx, email, item, msg)
	case "cancel":
		return handleCancel(ctx, email, item)
	default:
		logger.Warnf("[ROUTER] Unknown task state: %s", item.State)
		return 0, nil
	}
}

func handleNone() (int, error) {
	return 0, nil
}

func handleNew(ctx context.Context, item TodoItem, msg ConsolidatedMessage) (int, error) {
	// Why: Fallback to original message task if AI didn't provide a specific rewrite (common in manual test calls).
	if item.Task == "" {
		item.Task = msg.Task
	}

	if msg.ID != 0 {
		// Why: If the message already exists in DB, update its task field with the AI-extracted text.
		return msg.ID, UpdateTaskText(ctx, msg.UserEmail, msg.ID, item.Task)
	}
	msg.Task = item.Task
	_, id, err := SaveMessage(ctx, msg)
	return id, err
}

func handleUpdate(ctx context.Context, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("update requested but ID is nil")
	}

	id := int(*item.ID)
	date := time.Now().Format("2006-01-02")
	if err := UpdateTaskFullAppend(ctx, id, date, item.Task, msg.OriginalText); err != nil {
		return id, err
	}

	if item.AssignedTo != "" {
		_ = UpdateTaskAssignee(ctx, email, id, NormalizeName(email, item.AssignedTo))
	}

	// Why: Appends the source of the triggering message to the existing task's source_channels.
	if existing, err := GetMessageByID(ctx, email, id); err == nil {
		merged := append(existing.SourceChannels, msg.Source)
		_ = UpdateTaskSourceChannels(ctx, email, id, uniqueStrings(merged))
	}
	return id, nil
}

func handleResolve(ctx context.Context, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("resolve requested but ID is nil")
	}
	id := int(*item.ID)
	if err := MarkMessageDone(ctx, email, id, true); err != nil {
		return id, err
	}
	// Why: Appends the context of the resolution message to the task for audit trial.
	_ = UpdateTaskFullAppend(ctx, id, time.Now().Format("2006-01-02"), "[Resolved]", msg.OriginalText)
	return id, nil
}

func handleCancel(ctx context.Context, email string, item TodoItem) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("cancel requested but ID is nil")
	}
	id := int(*item.ID)
	err := DeleteMessages(ctx, email, []int{id})
	return 0, err
}

func uniqueStrings(input []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range input {
		if entry != "" && !keys[entry] {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}
