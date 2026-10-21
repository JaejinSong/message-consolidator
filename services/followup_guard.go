package services

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"time"
)

const (
	// followupWindowDays is how long after closing a task a same-requester message is
	// still checked as a possible reminder; prime so it never aligns with weekly/monthly cycles.
	followupWindowDays = 31
	// followupMinOverlap is stricter than minTopicalOverlap because the AI call is only
	// worth paying for when the titles share real substance (reminder pair measured 6, unrelated same-deal 4-5).
	followupMinOverlap = minTopicalOverlap + 2
	// followupAITimeout bounds the inline AI call so a hung provider cannot stall the scan loop.
	followupAITimeout = 41 * time.Second
	// followupMaxTextRunes caps the message sent to the model; prime so it never aligns with a round limit.
	followupMaxTextRunes = 1999
)

// IsReminderOfDone reports whether a would-be new task is only a reminder of a task the user
// already closed, appending the message to that closed task instead of creating a duplicate.
// Why: dedupe compares against active tasks only, so an automated reminder in a fresh thread
// resurrected a closed request (task 13477 -> 13504). Fails open: any doubt or AI error creates the task.
func (s *CompletionService) IsReminderOfDone(ctx context.Context, item store.TodoItem, msg store.ConsolidatedMessage) bool {
	if item.State != "new" || s.gemini == nil || s.hasOpenThreadTask(ctx, msg) {
		return false
	}
	requester := followupRequester(ctx, item, msg)
	if requester == "" {
		return false
	}
	done, ok := s.pickFollowupCandidate(ctx, item, msg, requester)
	if !ok {
		return false
	}

	aiCtx, cancel := context.WithTimeout(ctx, followupAITimeout)
	defer cancel()
	closedOn := ""
	if done.CompletedAt != nil {
		closedOn = done.CompletedAt.Format("2006-01-02")
	}
	verdict, err := s.gemini.EvaluateFollowup(aiCtx, msg.UserEmail, done.Task, closedOn, truncateRunes(msg.OriginalText, followupMaxTextRunes))
	if err != nil {
		logger.Warnf("[COMPLETION] followup check failed doneID=%d, creating task: %v", done.ID, err)
		return false
	}
	if verdict.Verdict != ai.FollowupReminder {
		return false
	}
	return s.absorbReminder(ctx, item, msg, done, verdict.Reason)
}

// hasOpenThreadTask skips the guard when the message's thread already has an open task.
// Why: that case is a thread fold/update owned by HandleTaskState, not a reminder of a closed request.
func (s *CompletionService) hasOpenThreadTask(ctx context.Context, msg store.ConsolidatedMessage) bool {
	if msg.ThreadID == "" {
		return false
	}
	open, err := s.store.GetIncompleteByThreadID(ctx, s.db, msg.UserEmail, msg.ThreadID)
	return err != nil || len(open) > 0
}

func followupRequester(ctx context.Context, item store.TodoItem, msg store.ConsolidatedMessage) string {
	raw := msg.Requester
	if raw == "" {
		raw = item.Requester
	}
	return store.NormalizeName(ctx, msg.UserEmail, raw)
}

// pickFollowupCandidate returns the closed task with the highest title overlap that clears followupMinOverlap.
func (s *CompletionService) pickFollowupCandidate(ctx context.Context, item store.TodoItem, msg store.ConsolidatedMessage, requester string) (store.ConsolidatedMessage, bool) {
	closed, err := store.GetRecentDoneBySource(ctx, msg.UserEmail, msg.Source, followupWindowDays)
	if err != nil {
		logger.Warnf("[COMPLETION] followup candidate lookup failed: %v", err)
		return store.ConsolidatedMessage{}, false
	}
	var best store.ConsolidatedMessage
	bestOverlap := followupMinOverlap - 1
	for _, c := range closed {
		// Why: AppendOriginalText matches on room, so an empty room updates 0 rows and the reminder would vanish.
		if c.Room == "" || !c.Done || c.IsDeleted || (msg.ThreadID != "" && c.ThreadID == msg.ThreadID) {
			continue
		}
		if store.NormalizeName(ctx, msg.UserEmail, c.Requester) != requester {
			continue
		}
		if overlap := titleTokenOverlap(item.Task, c.Task); overlap > bestOverlap {
			best, bestOverlap = c, overlap
		}
	}
	return best, best.ID != 0
}

func (s *CompletionService) absorbReminder(ctx context.Context, item store.TodoItem, msg store.ConsolidatedMessage, done store.ConsolidatedMessage, reason string) bool {
	if err := store.AppendOriginalText(ctx, s.db, msg.UserEmail, done.Room, done.ID, msg.OriginalText); err != nil {
		// Why: losing the reminder text is safer than losing the request, so fall back to creating the task.
		logger.Warnf("[COMPLETION] followup append failed doneID=%d, creating task: %v", done.ID, err)
		return false
	}
	doneID := int64(done.ID)
	logger.LogDecision(logger.DecisionLog{
		UserEmail: msg.UserEmail, Source: msg.Source, Room: msg.Room, State: "reminder_of_done",
		TaskID: &doneID, Task: item.Task,
		Reasoning: fmt.Sprintf("reminder of closed task %d: %s", done.ID, reason),
	})
	return true
}
