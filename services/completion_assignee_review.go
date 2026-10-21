package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
)

// ReviewFoldedAssignee asks the completion AI whether a message folded into an open
// thread parent moves the whole task to someone else, and applies only that owner change.
// Why: thread-fold routing never touches the assignee, so a chat reply like "Hady will
// handle this" left the task on the old owner. The AI call is gated on the extracted
// assignee differing from the parent's and being named in or sending the message.
func (s *CompletionService) ReviewFoldedAssignee(ctx context.Context, msg store.ConsolidatedMessage, parentID store.MessageID, extractedAssignee string) (bool, error) {
	if extractedAssignee == "" {
		return false, nil
	}
	parent, ok := s.findOpenThreadTask(ctx, msg, parentID)
	if !ok || fieldIsManual(parent.Metadata, "assignee") {
		return false, nil
	}
	normalized := store.NormalizeName(ctx, msg.UserEmail, extractedAssignee)
	if normalized == parent.Assignee || !isNamedOrSender(ctx, msg, extractedAssignee, normalized) {
		return false, nil
	}

	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, parent.Task, parent.Assignee, msg.OriginalText, parent.Subtasks)
	if err != nil {
		return false, fmt.Errorf("folded assignee review failed: %w", err)
	}
	logger.Infof("[COMPLETION] folded assignee review taskID=%d current=%q extracted=%q ai_status=%s ai_assigned_to=%q",
		parentID, parent.Assignee, extractedAssignee, res.Status, res.AssignedTo)
	// Why: assigned_to is only meaningful on a reassigning UPDATE; a stray value on RESOLVE/NONE must not move the task.
	if res.Status != "UPDATE" || res.AssignedTo == "" {
		return false, nil
	}

	// Why: empty Task routes to append-only, so the title the fold just wrote is never overwritten.
	item := store.TodoItem{State: "update", ID: &parentID, AssignedTo: res.AssignedTo}
	if _, err := s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg); err != nil {
		return false, fmt.Errorf("folded assignee apply failed: %w", err)
	}
	return true, nil
}

func (s *CompletionService) findOpenThreadTask(ctx context.Context, msg store.ConsolidatedMessage, id store.MessageID) (store.ConsolidatedMessage, bool) {
	tasks, err := s.store.GetIncompleteByThreadID(ctx, s.db, msg.UserEmail, msg.ThreadID)
	if err != nil {
		logger.Warnf("[COMPLETION] folded assignee review: parent lookup failed taskID=%d: %v", id, err)
		return store.ConsolidatedMessage{}, false
	}
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return store.ConsolidatedMessage{}, false
}

// isNamedOrSender is the cheap pre-gate before the AI call: a handover names the new owner
// ("Hady, please complete this") or comes from them ("I'll take this over").
// Why: in group chats most folds carry an assignee other than the owner without any handover.
func isNamedOrSender(ctx context.Context, msg store.ConsolidatedMessage, extracted, normalized string) bool {
	if msg.Requester != "" && store.NormalizeName(ctx, msg.UserEmail, msg.Requester) == normalized {
		return true
	}
	first := strings.Fields(extracted)
	return len(first) > 0 && strings.Contains(strings.ToLower(msg.OriginalText), strings.ToLower(first[0]))
}
