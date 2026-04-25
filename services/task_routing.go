package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"time"
)

// HandleTaskState routes task operations based on the AI-determined state.
// Why: Centralizes task state transitions to ensure consistency.
func HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	if q == nil {
		q = store.GetDB()
	}

	if item.Status != "" {
		item.State = item.Status
	}

	resID, err := routeTaskState(ctx, q, email, item, msg)

	logger.LogDecision(logger.DecisionLog{
		UserEmail: email,
		Source:    msg.Source,
		Room:      msg.Room,
		State:     item.State,
		TaskID:    item.ID,
		Task:      item.Task,
		Reasoning: item.Reasoning,
	})

	return resID, err
}

// RouteTaskByStatus provides a single-entry routing point for higher-level callers.
// Returns 0 for new/unhandled cases so the bulk INSERT pipeline takes over.
func RouteTaskByStatus(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	status := strings.ToLower(item.Status)
	if status == "resolve" || status == "done" {
		return handleResolve(ctx, q, email, item, msg)
	}
	if status == "update" && (msg.Category == string(types.CategoryTask) || msg.Category == "TASK") {
		return handleUpdate(ctx, q, email, item, msg)
	}
	if status == "cancel" {
		return handleCancel(ctx, q, email, item)
	}
	return 0, nil
}

func routeTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
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

func handleNew(ctx context.Context, q store.Querier, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	if item.Task == "" {
		item.Task = msg.Task
	}

	if len(item.Subtasks) > 0 {
		msg.Subtasks = mapTodoSubtasksToStore(item.Subtasks)
	}

	if msg.ID != 0 {
		err := store.UpdateTaskText(ctx, q, msg.UserEmail, msg.ID, item.Task)
		if err == nil && len(msg.Subtasks) > 0 {
			_ = store.UpdateSubtasks(ctx, q, msg.UserEmail, msg.ID, msg.Subtasks)
		}
		return msg.ID, err
	}

	if msg.ThreadID != "" {
		if existing, _ := store.GetIncompleteByThreadID(ctx, q, msg.UserEmail, msg.ThreadID); len(existing) > 0 {
			id := existing[0].ID
			err := store.UpdateTaskText(ctx, q, msg.UserEmail, id, item.Task)
			if err == nil && len(msg.Subtasks) > 0 {
				_ = store.UpdateSubtasks(ctx, q, msg.UserEmail, id, msg.Subtasks)
			}
			return id, err
		}
	}

	msg.Task = item.Task
	if item.Requester != "" {
		msg.Requester = item.Requester
	}
	if item.Assignee != "" {
		msg.Assignee = item.Assignee
	}
	if item.AssigneeReason != "" {
		msg.AssigneeReason = item.AssigneeReason
	}
	_, id, err := store.SaveMessage(ctx, q, msg)
	return id, err
}

func handleUpdate(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("update requested but ID is nil")
	}
	id := int(*item.ID)

	existing, err := validateTargetTask(ctx, q, email, id, msg.Room)
	if err != nil || existing == nil {
		return 0, err
	}

	if len(item.Subtasks) > 0 {
		_ = store.UpdateSubtasks(ctx, q, email, id, mapTodoSubtasksToStore(item.Subtasks))
	}

	if err := store.UpdateTaskFullAppend(ctx, q, email, msg.Room, id, item.Task, msg.OriginalText); err != nil {
		return id, err
	}

	if item.AssignedTo != "" {
		normalized := store.NormalizeName(email, item.AssignedTo)
		// Why (Phase J Path B): @mention reassignment must bump assigned_at to the trigger
		// envelope timestamp so envelope metadata doesn't go stale. Same assignee = no-op.
		if existing.Assignee != normalized {
			_ = store.UpdateTaskAssigneeAndAssignedAt(ctx, q, email, id, normalized, msg.AssignedAt)
		}
	}

	merged := append(existing.SourceChannels, msg.Source)
	_ = store.UpdateTaskSourceChannels(ctx, q, email, id, uniqueStrings(merged))

	return id, nil
}

func handleResolve(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (int, error) {
	if q == nil {
		q = store.GetDB()
	}
	if item.ID == nil {
		return 0, fmt.Errorf("resolve requested but ID is nil")
	}
	id := int(*item.ID)

	existing, err := validateTargetTask(ctx, q, email, id, msg.Room)
	if err != nil || existing == nil {
		return 0, err
	}

	if err := store.MarkMessageDone(ctx, q, email, id, true); err != nil {
		return id, err
	}
	_ = store.AppendOriginalText(ctx, q, email, msg.Room, id, fmt.Sprintf("[Resolved: %s]\n%s", time.Now().Format("2006-01-02"), msg.OriginalText))
	return id, nil
}

func handleCancel(ctx context.Context, q store.Querier, email string, item store.TodoItem) (int, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("cancel requested but ID is nil")
	}
	id := int(*item.ID)
	err := store.DeleteMessages(ctx, q, email, []int{id})
	return 0, err
}

func mapTodoSubtasksToStore(todo []store.TodoSubtask) []store.Subtask {
	res := make([]store.Subtask, len(todo))
	for i, t := range todo {
		res[i] = store.Subtask{
			Task:     t.Task,
			Assignee: t.AssigneeName,
			Done:     false,
		}
	}
	return res
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

// validateTargetTask drops cross-room operations to prevent unauthorized modification.
// Returns nil, nil for the drop case so callers can continue harmlessly.
func validateTargetTask(ctx context.Context, q store.Querier, email string, id int, expectedRoom string) (*store.ConsolidatedMessage, error) {
	existing, err := store.GetMessageByID(ctx, q, email, id)
	if err != nil {
		logger.Errorf("[ROUTER] Failed to fetch task %d for validation: %v", id, err)
		return nil, err
	}
	if existing.Room != expectedRoom {
		logger.Errorf("[SECURITY] ID Cross-room operation attempted: Task %d (Room: %s) vs Incoming (Room: %s)", id, existing.Room, expectedRoom)
		return nil, nil
	}
	return &existing, nil
}
