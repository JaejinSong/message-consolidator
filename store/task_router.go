package store

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/types"
	"strings"
	"time"
)

// HandleTaskState routes task operations based on the AI-determined state.
// Why: Centralizes task state transitions to ensure consistency. Refactored into helpers to maintain 30-line limit.
func HandleTaskState(ctx context.Context, q Querier, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	if q == nil {
		q = GetDB()
	}

	// [Priority] Use 'status' field if provided by AI, fallback to legacy 'state'.
	if item.Status != "" {
		item.State = item.Status
	}

	resID, err := routeTaskState(ctx, q, email, item, msg)

	// Why: [Diagnosis] Standardize decision logging to detect silent mapping failures.
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

// RouteTaskByStatus provides a single-entry routing point for higher-level services.
// Only returns ID for valid updates/resolves; returns 0 for new tasks to be handled by bulk pipeline.
// Why: Standardizes 40-line limit and 2-level nesting constraints.
func RouteTaskByStatus(ctx context.Context, q Querier, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
	status := strings.ToLower(item.Status)
	// Why: [Consistency] Explicitly handles RESOLVE status which transforms any TASK into a DONE state.
	if status == "resolve" || status == "done" {
		return handleResolve(ctx, q, email, item, msg)
	}
	if status == "update" && (msg.Category == string(types.CategoryTask) || msg.Category == "TASK") {
		return handleUpdate(ctx, q, email, item, msg)
	}
	if status == "cancel" {
		return handleCancel(ctx, q, email, item)
	}
	return 0, nil // Indicates "new" or unhandled, proceed to INSERT
}

func routeTaskState(ctx context.Context, q Querier, email string, item TodoItem, msg ConsolidatedMessage) (int, error) {
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

	// Why: [Contextual Consolidation] Map AI-extracted subtasks to the persistent DB structure.
	if len(item.Subtasks) > 0 {
		msg.Subtasks = mapTodoSubtasksToStore(item.Subtasks)
	}

	if msg.ID != 0 {
		// Why: If the message already exists in DB, update its task field and subtasks.
		err := UpdateTaskText(ctx, q, msg.UserEmail, msg.ID, item.Task)
		if err == nil && len(msg.Subtasks) > 0 {
			_ = UpdateSubtasks(ctx, q, msg.UserEmail, msg.ID, msg.Subtasks)
		}
		return msg.ID, err
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
	
	// Why: [Security] Extracted common validation logic to ensure cross-room updates are dropped.
	existing, err := validateTargetTask(ctx, q, email, id, msg.Room)
	if err != nil || existing == nil {
		return 0, err
	}

	// Why: [Contextual Consolidation] Update subtasks if provided by AI during an 'update' cycle.
	if len(item.Subtasks) > 0 {
		_ = UpdateSubtasks(ctx, q, email, id, mapTodoSubtasksToStore(item.Subtasks))
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
	if q == nil {
		q = GetDB()
	}
	if item.ID == nil {
		return 0, fmt.Errorf("resolve requested but ID is nil")
	}
	id := int(*item.ID)

	existing, err := validateTargetTask(ctx, q, email, id, msg.Room)
	if err != nil || existing == nil {
		return 0, err
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

func mapTodoSubtasksToStore(todo []TodoSubtask) []Subtask {
	res := make([]Subtask, len(todo))
	for i, t := range todo {
		res[i] = Subtask{
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

// validateTargetTask ensures the target task belongs to the expected room to prevent unauthorized cross-room modification.
// Returns nil, nil if validation fails (drop message case), allowing upstream router to continue harmlessly.
func validateTargetTask(ctx context.Context, q Querier, email string, id int, expectedRoom string) (*ConsolidatedMessage, error) {
	existing, err := GetMessageByID(ctx, q, email, id)
	if err != nil {
		logger.Errorf("[ROUTER] Failed to fetch task %d for validation: %v", id, err)
		return nil, err
	}
	if existing.Room != expectedRoom {
		logger.Errorf("[SECURITY] ID Cross-room operation attempted: Task %d (Room: %s) vs Incoming (Room: %s)", id, existing.Room, expectedRoom)
		return nil, nil // Valid Drop & Continue
	}
	return &existing, nil
}
