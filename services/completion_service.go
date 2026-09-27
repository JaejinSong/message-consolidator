package services

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
)

type AICompleter interface {
	AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error)
	EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string, subtasks []store.Subtask) (ai.TaskTransition, error)
	Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error)
}

type TaskStore interface {
	GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error)
	HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error)
	GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error)
	GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error)
	UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error
	HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error)
	UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error
	AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error
	SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]store.ConsolidatedMessage, error)
}

type DefaultTaskStore struct{}

func (d *DefaultTaskStore) GetIncompleteByThreadID(ctx context.Context, q store.Querier, email, threadID string) ([]store.ConsolidatedMessage, error) {
	return store.GetIncompleteByThreadID(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) HasAnyTaskInThread(ctx context.Context, q store.Querier, email, threadID string) (bool, error) {
	return store.HasAnyTaskInThread(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) GetLatestThreadAssignee(ctx context.Context, q store.Querier, email, threadID string) (string, error) {
	return store.GetLatestThreadAssignee(ctx, q, email, threadID)
}

func (d *DefaultTaskStore) UpdateMessageCategory(ctx context.Context, q store.Querier, email string, id store.MessageID, category string) error {
	return store.UpdateMessageCategory(ctx, q, email, id, category)
}

func (d *DefaultTaskStore) HandleTaskState(ctx context.Context, q store.Querier, email string, item store.TodoItem, msg store.ConsolidatedMessage) (store.MessageID, error) {
	return HandleTaskState(ctx, q, email, item, msg)
}

func (d *DefaultTaskStore) GetRecentIncompleteGmail(ctx context.Context, q store.Querier, email string) ([]store.ConsolidatedMessage, error) {
	return store.GetRecentIncompleteGmail(ctx, q, email)
}

func (d *DefaultTaskStore) UpdateSubtasks(ctx context.Context, q store.Querier, email string, id store.MessageID, subtasks []store.Subtask) error {
	return store.UpdateSubtasks(ctx, q, email, id, subtasks)
}

func (d *DefaultTaskStore) AddCompletionCandidate(ctx context.Context, q store.Querier, email string, id store.MessageID, cand store.CompletionCandidate) error {
	return store.AddCompletionCandidate(ctx, q, email, id, cand)
}

func (d *DefaultTaskStore) SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]store.ConsolidatedMessage, error) {
	return store.SearchOpenTasksFTS(ctx, email, tokens, limit)
}

type CompletionService struct {
	gemini   AICompleter
	store    TaskStore
	tasksSvc *TasksService
	db       *sql.DB
}

func NewCompletionService(gemini AICompleter, taskStore TaskStore, tasksSvc *TasksService, db *sql.DB) *CompletionService {
	return &CompletionService{gemini: gemini, store: taskStore, tasksSvc: tasksSvc, db: db}
}

// ProcessCrossChannelSignal evaluates a plain chat message against open tasks in
// other threads/channels. Confirm-first only: records pending candidates, never
// auto-closes, never falls back to extraction.
func (s *CompletionService) ProcessCrossChannelSignal(ctx context.Context, msg store.ConsolidatedMessage) (bool, error) {
	if !hasCompletionSignal(msg.OriginalText) {
		compStats.crossSignalMiss.Add(1)
		return false, nil
	}
	candidates := s.findCrossThreadCandidates(ctx, msg)
	if len(candidates) == 0 {
		return false, nil
	}
	return s.handleCrossThreadCandidates(ctx, msg, candidates), nil
}

// ProcessPotentialCompletion checks if a message (reply) completes/updates tasks in the same thread.
// Why: [Early Return] Returns true if the message was handled as a task completion/update, signaling the scanner to skip extraction.
// processThreadWithoutTasks handles a reply landing on a thread that has no open task:
// look for a confirm-first match in another thread, then choose between skipping a pure
// self-summary and paying for a fresh extraction.
func (s *CompletionService) processThreadWithoutTasks(ctx context.Context, msg store.ConsolidatedMessage, targetID string) bool {
	// Why: cross-thread candidate check runs before the fromMe self-summary skip
	// below — previously that skip returned early and a fromMe self-summary could
	// never surface a confirm-first match against an open task in another thread.
	if candidates := s.findCrossThreadCandidates(ctx, msg); len(candidates) > 0 {
		if s.handleCrossThreadCandidates(ctx, msg, candidates) {
			return true
		}
	}
	if strings.EqualFold(msg.RequesterCanonical, msg.UserEmail) {
		// Why: skip only when this thread has NEVER had a task (truly a self-summary
		// like a weekly report). If any prior task exists (incl. done), the user's
		// follow-up is a reopen signal — let normal extraction run.
		hasAny, _ := s.store.HasAnyTaskInThread(ctx, s.db, msg.UserEmail, targetID)
		if !hasAny {
			return true
		}
	}
	// Why: Fallback consumes its own AI Analyze + persists tasks. Returning true
	// signals the caller to MarkAsProcessed so the next scan cycle skips this msg
	// instead of paying for LiteFilter + Analyze + batch Analyze again.
	return s.fallbackToNewExtraction(ctx, msg)
}

// markTasksRequested files every open task in the thread back under Requested without
// calling AI. Why: Ack-only fromMe replies ("ok", "감사합니다", etc.) must not reach AI
// because a RESOLVE response would incorrectly close the sender's own task. The explicit
// ✅ dashboard button is the correct close path for these.
func (s *CompletionService) markTasksRequested(ctx context.Context, msg store.ConsolidatedMessage, tasks []store.ConsolidatedMessage) {
	for _, task := range tasks {
		_ = s.store.UpdateMessageCategory(ctx, s.db, msg.UserEmail, task.ID, CategoryRequested)
	}
}

// applyTransition fans one transition verdict out over every open task in the thread.
// Why: a single reply affects every open item from that conversation.
func (s *CompletionService) applyTransition(ctx context.Context, res ai.TaskTransition, msg store.ConsolidatedMessage, tasks []store.ConsolidatedMessage) bool {
	handled := false
	for _, task := range tasks {
		if s.handleCompletionResult(ctx, res, msg, task) {
			handled = true
		}
	}
	return handled
}

func (s *CompletionService) ProcessPotentialCompletion(ctx context.Context, msg store.ConsolidatedMessage) (bool, error) {
	if msg.ThreadID == "" && msg.RepliedToID == "" {
		return false, nil
	}
	targetID := msg.ThreadID
	if targetID == "" {
		targetID = msg.RepliedToID
	}

	tasks, _ := s.store.GetIncompleteByThreadID(ctx, s.db, msg.UserEmail, targetID)
	if len(tasks) == 0 {
		return s.processThreadWithoutTasks(ctx, msg, targetID), nil
	}

	compStats.entryThreadPath.Add(1)
	if strings.EqualFold(msg.RequesterCanonical, msg.UserEmail) {
		if isAckOnlyReply(msg.OriginalText) {
			s.markTasksRequested(ctx, msg, tasks)
			return true, nil
		}
		// Why: Substantive fromMe replies (redirect/delegation/resolution) need AI
		// judgment. With multiple tasks, evaluate each independently so a partial
		// resolution does not blindly close unrelated sibling tasks.
		if len(tasks) > 1 {
			return s.evaluatePerTask(ctx, msg, tasks)
		}
		// Single-task: fall through to the shared single-call path below.
	}

	res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, tasks[0].Task, msg.OriginalText, tasks[0].Subtasks)
	if err != nil {
		return false, fmt.Errorf("transition analysis failed: %w", err)
	}
	return s.applyTransition(ctx, res, msg, tasks), nil
}

// EvaluateThreadReply evaluates a same-thread reply from someone other than the task's
// own user against every open task in that thread, without the ProcessCrossChannelSignal
// keyword gate. Why: production evidence showed real completions are almost always a
// plain ack or answer inside the task's own thread ("Got it.", "Correct."), which never
// reached AI because the keyword gate only lets explicit completion wording through.
// RESOLVE from the task's own assignee hard-closes (same trust as a fromMe reply);
// RESOLVE from anyone else is recorded as a confirm-first candidate. UPDATE applies
// directly to the evaluated task; NONE is a no-op.
func (s *CompletionService) EvaluateThreadReply(ctx context.Context, msg store.ConsolidatedMessage, tasks []store.ConsolidatedMessage) (bool, error) {
	handled := false
	for _, task := range tasks {
		res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, task.Task, msg.OriginalText, task.Subtasks)
		if err != nil {
			return handled, fmt.Errorf("thread reply transition failed: %w", err)
		}
		switch res.Status {
		case "RESOLVE":
			if senderIsAssignee(msg.Requester, task.Assignee) {
				if s.handleCompletionResult(ctx, res, msg, task) {
					handled = true
				}
				continue
			}
			if s.recordCompletionCandidate(ctx, msg, task) {
				handled = true
			}
		case "UPDATE":
			if s.handleCompletionResult(ctx, res, msg, task) {
				handled = true
			}
		}
	}
	return handled, nil
}

// evaluatePerTask calls EvaluateTaskTransition individually for each task so that
// substantive fromMe multi-task replies can resolve some tasks while leaving others open.
func (s *CompletionService) evaluatePerTask(ctx context.Context, msg store.ConsolidatedMessage, tasks []store.ConsolidatedMessage) (bool, error) {
	handled := false
	for _, task := range tasks {
		res, err := s.gemini.EvaluateTaskTransition(ctx, msg.UserEmail, task.Task, msg.OriginalText, task.Subtasks)
		if err != nil {
			return false, fmt.Errorf("transition analysis failed: %w", err)
		}
		if s.handleCompletionResult(ctx, res, msg, task) {
			handled = true
		}
	}
	return handled, nil
}

// resolveSubtasks marks every subtask done. Why: cascade subtasks before the parent is
// resolved so reverse-propagation (all subtasks done -> parent auto-close) sees a
// consistent terminal state.
func (s *CompletionService) resolveSubtasks(ctx context.Context, email string, parent store.ConsolidatedMessage) {
	if len(parent.Subtasks) == 0 {
		return
	}
	allDone := make([]store.Subtask, len(parent.Subtasks))
	copy(allDone, parent.Subtasks)
	for i := range allDone {
		allDone[i].Done = true
	}
	_ = s.store.UpdateSubtasks(ctx, s.db, email, parent.ID, allDone)
}

// applySubtaskUpdates writes the AI's per-subtask done flags, ignoring out-of-range
// indices so a malformed response cannot panic or corrupt neighbouring subtasks.
func (s *CompletionService) applySubtaskUpdates(ctx context.Context, email string, parent store.ConsolidatedMessage, updates []ai.SubtaskUpdate) {
	if len(updates) == 0 || len(parent.Subtasks) == 0 {
		return
	}
	updated := make([]store.Subtask, len(parent.Subtasks))
	copy(updated, parent.Subtasks)
	for _, su := range updates {
		if su.Index >= 0 && su.Index < len(updated) {
			updated[su.Index].Done = su.Done
		}
	}
	_ = s.store.UpdateSubtasks(ctx, s.db, email, parent.ID, updated)
}

func (s *CompletionService) handleCompletionResult(ctx context.Context, res ai.TaskTransition, msg, parent store.ConsolidatedMessage) bool {
	parentID := parent.ID
	switch res.Status {
	case "RESOLVE":
		s.resolveSubtasks(ctx, msg.UserEmail, parent)
		item := store.TodoItem{State: "resolve", ID: &parentID}
		_, _ = s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
		return true
	case "UPDATE":
		if res.UpdatedText == "" {
			return false
		}
		s.applySubtaskUpdates(ctx, msg.UserEmail, parent, res.SubtaskUpdates)
		item := store.TodoItem{State: "update", ID: &parentID, Task: res.UpdatedText}
		_, _ = s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
		return true
	case "NEW":
		return s.fallbackToNewExtraction(ctx, msg)
	}
	return false
}

// fallbackToNewExtraction runs an isolated AI extraction for messages whose thread
// has no incomplete parent task. Returns true once AI Analyze succeeds so callers
// can MarkAsProcessed and avoid paying tokens again next scan cycle (the prior
// void return left the message in filteredMsgs, causing a second batch Analyze
// in processBatch within the same cycle and re-extraction every cycle thereafter).
func (s *CompletionService) fallbackToNewExtraction(ctx context.Context, msg store.ConsolidatedMessage) bool {
	// Why: thread had a real (done) parent task — propagate its assignee so the
	// new row inherits routing context. AI per-item Assignee still wins via
	// createTaskFromItem override; this only fills the envelope default.
	if msg.Assignee == "" && msg.ThreadID != "" {
		if a, err := s.store.GetLatestThreadAssignee(ctx, s.db, msg.UserEmail, msg.ThreadID); err == nil && a != "" {
			msg.Assignee = a
		}
	}
	enriched := types.EnrichedMessage{
		RawContent: msg.OriginalText, SourceChannel: msg.Source,
		SenderName: msg.Requester, VirtualThreadID: msg.ThreadID, Timestamp: msg.CreatedAt,
	}
	room := msg.Room
	if room == "" {
		room = "General"
	}
	items, err := s.gemini.Analyze(ctx, msg.UserEmail, enriched, "Korean", msg.Source, room)
	if err != nil || len(items) == 0 {
		return false
	}

	// Why: items are independent SaveMessage calls — wrapping them in a single
	// outer tx only widened the libsql writer-lock window and silently swallowed
	// per-item INSERT failures via tx.Commit() on a nil-return fn. WithDBRetry
	// absorbs transient `database is locked` errors (5 attempts, 100ms→1.6s
	// backoff). AI cost is sunk regardless of save outcome — the bool return is
	// what stops the token bleed, not save success.
	for _, item := range items {
		err := store.WithDBRetry("CompletionFallback.HandleTaskState", func() error {
			_, e := s.store.HandleTaskState(ctx, s.db, msg.UserEmail, item, msg)
			return e
		})
		if err != nil {
			logger.Warnf("[COMPLETION] fallback: HandleTaskState dropped item after retries: %v", err)
		}
	}
	compStats.fallbackExtraction.Add(1)
	return true
}
