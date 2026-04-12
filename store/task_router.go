package store

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"time"
)

// HandleTaskState routes task operations based on the AI-determined state.
// Why: Centralizes task state transitions to ensure consistency. Refactored into helpers to maintain 30-line limit.
func HandleTaskState(ctx context.Context, q Querier, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if q == nil {
		q = GetDB()
	}

	switch item.State {
	case "none":
		return handleNone()
	case "new":
		return handleNew(ctx, q, item, msg)
	case "update":
		return handleUpdate(ctx, q, email, item, msg)
	case "resolve":
		return handleResolve(ctx, q, email, item, msg)
	case "cancel":
		return handleCancel(ctx, q, email, item)
	default:
		logger.Warnf("[ROUTER] Unknown task state: %s", item.State)
		return 0, nil
	}
}

func handleNone() (int, error) {
	return 0, nil
}

func handleNew(ctx context.Context, q Querier, item TodoItem, msg ConsolidatedMessage) (int, error) {
	// Why: Fallback to original message task if AI didn't provide a specific rewrite (common in manual test calls).
	if item.Task == "" {
		item.Task = msg.Task
	}

	if msg.ID != 0 {
		// Why: If the message already exists in DB, update its task field with the AI-extracted text.
		return msg.ID, UpdateTaskText(ctx, q, msg.UserEmail, msg.ID, item.Task)
	}
	msg.Task = item.Task
	_, id, err := SaveMessage(ctx, q, msg)
	return id, err
}

func handleUpdate(ctx context.Context, q Querier, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("update requested but ID is nil")
	}

	id := int(*item.ID)
	// Why: [Security] Validate that the task being updated belongs to the same room as the incoming message.
	existing, err := GetMessageByID(ctx, q, email, id)
	if err != nil {
		logger.Errorf("[ROUTER] Failed to fetch task %d for validation: %v", id, err)
		return 0, err
	}

	if existing.Room != msg.Room {
		logger.Errorf("[SECURITY] ID Cross-room update attempted: Task %d (Room: %s) vs Incoming (Room: %s)", id, existing.Room, msg.Room)
		return 0, nil // Drop & Continue pattern
	}

	date := time.Now().Format("2006-01-02")
	if err := UpdateTaskFullAppend(ctx, q, email, msg.Room, id, date, item.Task, msg.OriginalText); err != nil {
		return id, err
	}

	if item.AssignedTo != "" {
		_ = UpdateTaskAssignee(ctx, q, email, id, NormalizeName(email, item.AssignedTo))
	}

	// Why: Appends the source of the triggering message to the existing task's source_channels.
	merged := append(existing.SourceChannels, msg.Source)
	_ = UpdateTaskSourceChannels(ctx, q, email, id, uniqueStrings(merged))
	
	return id, nil
}

func handleResolve(ctx context.Context, q Querier, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("resolve requested but ID is nil")
	}
	id := int(*item.ID)

	// Why: [Security] Validate that the task being resolved belongs to the same room.
	existing, err := GetMessageByID(ctx, q, email, id)
	if err != nil {
		return 0, err
	}
	if existing.Room != msg.Room {
		logger.Errorf("[SECURITY] ID Cross-room resolve attempted: Task %d (Room: %s) vs Incoming (Room: %s)", id, existing.Room, msg.Room)
		return 0, nil
	}

	if err := MarkMessageDone(ctx, q, email, id, true); err != nil {
		return id, err
	}
	// Why: Appends the context of the resolution message to the task for audit trial.
	_ = UpdateTaskFullAppend(ctx, q, email, msg.Room, id, time.Now().Format("2006-01-02"), "[Resolved]", msg.OriginalText)
	return id, nil
}

func handleCancel(ctx context.Context, q Querier, email string, item TodoItem) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("cancel requested but ID is nil")
	}
	id := int(*item.ID)
	err := DeleteMessages(ctx, q, email, []int{id})
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
