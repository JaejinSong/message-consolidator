package services

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"
)

// resolvedPrefixMarker identifies an already-resolved task's appended audit line.
// Why: handleResolve prepends "[Resolved: YYYY-MM-DD]" to original_text on every
// resolve trigger; without this guard a re-fired resolve (e.g. duplicate AI
// signal, completion-service + scanner overlap) would prepend the prefix again.
const resolvedPrefixMarker = "[Resolved:"

// runTaskTx wraps a multi-statement task transition in a transaction so partial
// writes can't leak. If the caller already passes *sql.Tx, the existing tx is
// reused (caller owns commit/rollback). Nil or *sql.DB ⇒ start a new tx.
func runTaskTx(ctx context.Context, q store.Querier, fn func(q store.Querier) error) error {
	if tx, ok := q.(*sql.Tx); ok {
		return fn(tx)
	}
	return store.RunInTx(ctx, func(tx *sql.Tx) error {
		return fn(tx)
	})
}

// HandleTaskState routes task operations based on the AI-determined state.
// Why: Centralizes task state transitions to ensure consistency.
func HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	if q == nil {
		q = store.GetDB()
	}

	if item.Status != "" {
		item.State = item.Status
	}

	resID, err := routeTaskState(ctx, q, email, item, msg)

	var taskIDPtr *int64
	if item.ID != nil {
		raw := int64(*item.ID)
		taskIDPtr = &raw
	}
	logger.LogDecision(logger.DecisionLog{
		UserEmail: email,
		Source:    msg.Source,
		Room:      msg.Room,
		State:     item.State,
		TaskID:    taskIDPtr,
		Task:      item.Task,
		Reasoning: item.Reasoning,
	})

	return resID, err
}

func routeTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
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

func handleNone() (store.MessageID, error) {
	return 0, nil
}

// autoRestoreExcluded un-parks a tracking-excluded task before applying inbound updates.
// Why: best-effort — a restore failure must not block the content update itself.
func autoRestoreExcluded(ctx context.Context, q store.Querier, email string, id store.MessageID) {
	restored, err := store.AutoRestoreIfExcluded(ctx, q, email, id)
	if err != nil {
		logger.Warnf("[ROUTER] auto-restore excluded failed msg=%d: %v", id, err)
		return
	}
	if restored {
		logger.Infof("[ROUTER] excluded task auto-restored by new activity msg=%d", id)
	}
}

func handleNew(ctx context.Context, q store.Querier, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	if item.Task == "" {
		item.Task = msg.Task
	}
	if len(item.Subtasks) > 0 {
		msg.Subtasks = mapTodoSubtasksToStore(item.Subtasks)
	}
	if msg.ID != 0 {
		return updateExistingTask(ctx, q, msg.UserEmail, msg.ID, item.Task, msg.Subtasks)
	}
	if id, ok, err := updateThreadParentIfPresent(ctx, q, msg, item.Task); ok {
		return id, err
	}
	return createTaskFromItem(ctx, q, item, msg)
}

// Why: Whichever path lands on an existing task ID applies the same text+subtask update; consolidate so handleNew has one branch instead of two.
func updateExistingTask(ctx context.Context, q store.Querier, email string, id store.MessageID, task string, subtasks []store.Subtask) (store.MessageID, error) {
	autoRestoreExcluded(ctx, q, email, id)
	err := store.UpdateTaskText(ctx, q, email, id, task)
	if err == nil && len(subtasks) > 0 {
		_ = store.UpdateSubtasks(ctx, q, email, id, subtasks)
	}
	return id, err
}

// Why: Resolves to an existing thread-parent task when the message has no explicit ID; returns ok=false so handleNew falls through to creation.
func updateThreadParentIfPresent(ctx context.Context, q store.Querier, msg store.ConsolidatedMessage, task string) (store.MessageID, bool, error) {
	if msg.ThreadID == "" {
		return 0, false, nil
	}
	existing, _ := store.GetIncompleteByThreadID(ctx, q, msg.UserEmail, msg.ThreadID)
	if len(existing) == 0 {
		return 0, false, nil
	}
	id, err := updateExistingTask(ctx, q, msg.UserEmail, existing[0].ID, task, msg.Subtasks)
	return id, true, err
}

// Why: Folds the SaveMessage path so handleNew's body stays linear. AI-supplied requester/assignee/reason override the envelope when present.
func createTaskFromItem(ctx context.Context, q store.Querier, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
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
	if msg.Requester != "" {
		if err := store.EnsureContactAlias(ctx, msg.UserEmail, msg.Requester); err != nil {
			logger.Warnf("[ROUTER] EnsureContactAlias requester: %v", err)
		}
	}
	if msg.Assignee != "" {
		if err := store.EnsureContactAlias(ctx, msg.UserEmail, msg.Assignee); err != nil {
			logger.Warnf("[ROUTER] EnsureContactAlias assignee: %v", err)
		}
	}
	_, id, err := store.SaveMessage(ctx, q, msg)
	return id, err
}

func handleUpdate(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("update requested but ID is nil")
	}
	id := *item.ID

	// Why: NormalizeName issues its own DB read (resolveContactIdentity → GetDB().QueryContext).
	// Inside runTaskTx the tx already holds the only test conn (maxOpen=1, modernc in-memory),
	// so the normalize-time conn lookup deadlocks until its 3s WithTimeout expires. Pre-normalize
	// outside the tx — read-only, doesn't depend on tx state — also halves conn pressure in prod.
	var normalizedAssignee string
	if item.AssignedTo != "" {
		normalizedAssignee = store.NormalizeName(ctx, email, item.AssignedTo)
	}

	var dropped bool
	err := runTaskTx(ctx, q, func(q store.Querier) error {
		existing, err := validateTargetTask(ctx, q, email, id, msg.Room)
		if err != nil {
			return err
		}
		if existing == nil {
			dropped = true
			return nil
		}
		return applyTaskUpdates(ctx, q, email, id, item, msg, existing, normalizedAssignee)
	})
	if err != nil || dropped {
		return 0, err
	}
	return id, nil
}

func applyTaskUpdates(ctx context.Context, q store.Querier, email string, id store.MessageID, item store.TodoItem, msg store.ConsolidatedMessage, existing *store.ConsolidatedMessage, normalizedAssignee string) error {
	autoRestoreExcluded(ctx, q, email, id)
	if len(item.Subtasks) > 0 {
		if err := store.UpdateSubtasks(ctx, q, email, id, mapTodoSubtasksToStore(item.Subtasks)); err != nil {
			return err
		}
	}
	if err := store.UpdateTaskFullAppend(ctx, q, email, msg.Room, id, item.Task, msg.OriginalText); err != nil {
		return err
	}
	if err := applyAssigneeChange(ctx, q, email, id, item, msg, existing, normalizedAssignee); err != nil {
		return err
	}
	merged := append(existing.SourceChannels, msg.Source)
	return store.UpdateTaskSourceChannels(ctx, q, email, id, uniqueStrings(merged))
}

// Why (Phase J Path B): @mention reassignment must bump assigned_at to the trigger
// envelope timestamp so envelope metadata doesn't go stale. Same assignee = no-op.
func applyAssigneeChange(ctx context.Context, q store.Querier, email string, id store.MessageID, item store.TodoItem, msg store.ConsolidatedMessage, existing *store.ConsolidatedMessage, normalizedAssignee string) error {
	if item.AssignedTo == "" {
		return nil
	}
	if existing.Assignee == normalizedAssignee {
		return nil
	}
	return store.UpdateTaskAssigneeAndAssignedAt(ctx, q, email, id, normalizedAssignee, msg.AssignedAt)
}

func handleResolve(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("resolve requested but ID is nil")
	}
	id := *item.ID
	var dropped bool

	err := runTaskTx(ctx, q, func(q store.Querier) error {
		existing, err := validateTargetTask(ctx, q, email, id, msg.Room)
		if err != nil || existing == nil {
			dropped = true
			return err
		}
		if err := store.MarkMessageDone(ctx, q, email, id, true); err != nil {
			return err
		}
		if strings.Contains(existing.OriginalText, resolvedPrefixMarker) {
			return nil
		}
		return store.AppendOriginalText(ctx, q, email, msg.Room, id, fmt.Sprintf("[Resolved: %s]\n%s", time.Now().Format("2006-01-02"), msg.OriginalText))
	})
	if err != nil || dropped {
		return 0, err
	}
	// Why: AI-driven resolve flips done=1 inside a transaction routed through free
	// functions, so the embedding enqueue is intentionally skipped here. Manual
	// MarkDone covers the typical archive transition; AI-resolved tasks get vectors
	// from the admin backfill endpoint, which sweeps any rows missing for the
	// configured model.
	return id, nil
}

func handleCancel(ctx context.Context, q store.Querier, email string, item store.TodoItem) (store.MessageID, error) {
	if item.ID == nil {
		return 0, fmt.Errorf("cancel requested but ID is nil")
	}
	id := *item.ID
	err := store.DeleteMessages(ctx, q, email, []store.MessageID{id})
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
func validateTargetTask(ctx context.Context, q store.Querier, email string, id store.MessageID, expectedRoom string) (*store.ConsolidatedMessage, error) {
	existing, err := store.GetMessageByID(ctx, q, email, id)
	if err != nil {
		logger.Errorf("[ROUTER] Failed to fetch task %d for validation: %v", id, err)
		return nil, err
	}
	if existing.Room != expectedRoom {
		logger.Errorf("[ROUTER] security: cross-room operation attempted: Task %d (Room: %s) vs Incoming (Room: %s)", id, existing.Room, expectedRoom)
		return nil, nil
	}
	return &existing, nil
}
