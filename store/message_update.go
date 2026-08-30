package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/logger"
	"strings"
	"time"
)

func executeUpdateMessageDetails(ctx context.Context, q Querier, email string, id MessageID, updateFn func(*db.UpdateMessageDetailsParams)) error {
	return withTx(ctx, q, func(qw Querier) error {
		params := db.UpdateMessageDetailsParams{
			ID:        int64(id),
			UserEmail: nullString(email),
		}
		updateFn(&params)
		if err := db.New(qw).UpdateMessageDetails(ctx, params); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

// CompletionCandidate is a confirm-first signal that an incoming cross-channel
// message likely completes this task. It is stored under metadata.completion_candidate
// and surfaced in the UI for one-tap confirmation — it never auto-closes the task.
type CompletionCandidate struct {
	SourceLink string  `json:"source_link,omitempty"`
	SourceText string  `json:"source_text,omitempty"`
	Confidence float64 `json:"confidence"`
	Evidence   string  `json:"evidence,omitempty"`
	DetectedAt string  `json:"detected_at"`
	Status     string  `json:"status"` // "pending"
}

// AddCompletionCandidate records a pending completion candidate on a task's metadata.
// Why: raw json_set is the cleanest expression — it merges into existing metadata
// without a read-modify-write race, and sqlc cannot represent JSON path updates.
func AddCompletionCandidate(ctx context.Context, q Querier, email string, id MessageID, cand CompletionCandidate) error {
	payload, err := json.Marshal(cand)
	if err != nil {
		return fmt.Errorf("marshal completion candidate: %w", err)
	}
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = `UPDATE messages
			SET metadata = json_set(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `', json(?))
			WHERE id = ? AND user_email = ?`
		if _, err := qw.ExecContext(ctx, stmt, string(payload), int64(id), email); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

// DismissCompletionCandidate removes a pending completion candidate from a task's
// metadata and records the dismissal marker (source link + timestamp). Why: the
// user rejected the confirm-first suggestion — the task stays open, must not keep
// surfacing the candidate, and must not resurface the same source_link later
// (see WasCandidateDismissed).
func DismissCompletionCandidate(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = `UPDATE messages SET metadata = json_set(
				json_remove(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `'),
				'$.` + metaKeyCompletionDismissedSource + `', json_extract(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `.source_link'),
				'$.` + metaKeyCompletionDismissedAt + `', ?
			)
			WHERE id = ? AND user_email = ?`
		dismissedAt := time.Now().UTC().Format(time.RFC3339)
		if _, err := qw.ExecContext(ctx, stmt, dismissedAt, int64(id), email); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

// WasCandidateDismissed reports whether sourceLink was already dismissed as a
// completion candidate for this task, so recordCompletionCandidate does not
// resurrect a suggestion the user explicitly rejected.
func WasCandidateDismissed(metadata, sourceLink string) bool {
	if sourceLink == "" {
		return false
	}
	return ParseMetadata(metadata).String(metaKeyCompletionDismissedSource) == sourceLink
}

func MarkMessageDone(ctx context.Context, q Querier, email string, id MessageID, done bool) error {
	if !done {
		return unmarkMessageDone(ctx, q, email, id)
	}
	return markMessageDoneTrue(ctx, q, email, id)
}

// markMessageDoneTrue sets done+completed_at and clears any pending completion
// candidate in one statement — Why: raw SQL (precedent: unmarkMessageDone) since
// UpdateMessageDetails cannot express the json_remove; a task explicitly marked
// done should not keep surfacing a stale confirm-first suggestion.
func markMessageDoneTrue(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = `UPDATE messages
			SET done = 1, completed_at = ?,
			    metadata = json_remove(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `'),
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND user_email = ?`
		if _, err := qw.ExecContext(ctx, stmt, time.Now(), int64(id), email); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

// unmarkMessageDone clears done, completed_at, and any pending completion candidate
// in one statement. Why: UpdateMessageDetails wraps completed_at in COALESCE(?,
// completed_at), so passing sql.NullTime{Valid:false} preserves the prior value
// instead of clearing it — leaving the invariant `done=0 ⇔ completed_at IS NULL`
// violated. Raw SQL is the cleanest expression here since sqlc cannot represent
// "set NULL".
func unmarkMessageDone(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = `UPDATE messages
			SET done = 0, completed_at = NULL,
			    metadata = json_remove(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `')
			WHERE id = ? AND user_email = ?`
		if _, err := qw.ExecContext(ctx, stmt, int64(id), email); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

// UpdateMessageCorrectionFields carries the optional fields a user-facing correction
// edit may touch. Zero-value (Valid: false) means "leave unchanged" -- callers must
// not populate fields the user did not intend to edit.
type UpdateMessageCorrectionFields struct {
	Task             sql.NullString
	Assignee         sql.NullString
	Category         sql.NullString
	Deadline         sql.NullString
	DeadlineDate     sql.NullTime
	DeadlineInferred sql.NullInt64
	Metadata         sql.NullString
}

// UpdateMessageCorrection applies a manual correction (task/assignee/category/deadline
// and, separately, the field_sources metadata marker) in one write. Why: unlike
// UpdateMessageDetails, this path must also persist deadline/metadata, which sqlc
// cannot share with the done/completed_at update shape.
func UpdateMessageCorrection(ctx context.Context, q Querier, email string, id MessageID, f UpdateMessageCorrectionFields) error {
	return withTx(ctx, q, func(qw Querier) error {
		if err := db.New(qw).UpdateMessageCorrection(ctx, db.UpdateMessageCorrectionParams{
			ID:               int64(id),
			UserEmail:        nullString(email),
			Task:             f.Task,
			Assignee:         f.Assignee,
			Category:         f.Category,
			Deadline:         f.Deadline,
			DeadlineDate:     f.DeadlineDate,
			DeadlineInferred: f.DeadlineInferred,
			Metadata:         f.Metadata,
		}); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

func UpdateTaskText(ctx context.Context, q Querier, email string, id MessageID, task string) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}
	// Why: Empty task hides the row from the active list. AI extractions sometimes
	// return blank task text on update/new states; preserve the existing title in
	// that case rather than silently wiping it.
	if strings.TrimSpace(task) == "" {
		logger.Warnf("[STORE] UpdateTaskText skipped: empty task for id=%d email=%s", id, email)
		return nil
	}
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Task = nullString(task)
	})
}

// UpdateSubtaskStatus toggles the 'done' status of a specific subtask within a consolidated task.
// Why: [Data Integrity] Loads the entire task to update the JSON subtasks array safely within a transaction.
func UpdateSubtaskStatus(ctx context.Context, q Querier, email string, id MessageID, subtaskIndex int, done bool) error {
	return withTx(ctx, q, func(qw Querier) error {
		return updateSubtaskStatusInternal(ctx, qw, email, id, subtaskIndex, done)
	})
}

// UpdateSubtasks replaces the entire subtask list for a message.
func UpdateSubtasks(ctx context.Context, q Querier, email string, id MessageID, subtasks []Subtask) error {
	subtasksJSON, err := json.Marshal(subtasks)
	if err != nil {
		return fmt.Errorf("failed to marshal subtasks: %w", err)
	}

	err = db.New(q).UpdateSubtasks(ctx, db.UpdateSubtasksParams{
		Subtasks:  nullString(string(subtasksJSON)),
		ID:        int64(id),
		UserEmail: nullString(email),
	})
	if err == nil {
		InvalidateCacheActive(email)
	}
	return err
}

func updateSubtaskStatusInternal(ctx context.Context, q Querier, email string, id MessageID, subtaskIndex int, done bool) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}

	queries := db.New(q)
	msgRow, err := queries.GetMessageByID(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("failed to fetch task: %w", err)
	}

	if msgRow.UserEmail != email {
		return fmt.Errorf("unauthorized access to task %d", id)
	}

	_, _, _, subtasks := UnmarshalMessageComponents("", "", "", msgRow.Subtasks)
	if subtaskIndex < 0 || subtaskIndex >= len(subtasks) {
		return fmt.Errorf("invalid subtask index: %d (total: %d)", subtaskIndex, len(subtasks))
	}

	subtasks[subtaskIndex].Done = done

	subtasksJSON, err := json.Marshal(subtasks)
	if err != nil {
		return fmt.Errorf("failed to marshal subtasks: %w", err)
	}

	err = queries.UpdateSubtasks(ctx, db.UpdateSubtasksParams{
		Subtasks:  nullString(string(subtasksJSON)),
		ID:        int64(id),
		UserEmail: nullString(email),
	})
	if err != nil {
		return fmt.Errorf("failed to update subtasks in DB: %w", err)
	}

	// Why: Reverse propagation — auto-close parent when last subtask is checked off.
	if done && allSubtasksDone(subtasks) {
		if err := MarkMessageDone(ctx, q, email, id, true); err != nil {
			logger.Warnf("[STORE] subtask auto-close parent %d: %v", id, err)
		}
	}

	InvalidateCacheActive(email)
	return nil
}

func allSubtasksDone(subtasks []Subtask) bool {
	if len(subtasks) == 0 {
		return false // no subtasks → never auto-close
	}
	for _, s := range subtasks {
		if !s.Done {
			return false
		}
	}
	return true
}

func UpdateMessageCategory(ctx context.Context, q Querier, email string, id MessageID, category string) error {
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Category = nullString(category)
	})
}

func UpdateTaskAssignee(ctx context.Context, q Querier, email string, id MessageID, assignee string) error {
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Assignee = nullString(assignee)
	})
}

// UpdateTaskAssigneeAndAssignedAt atomically updates the assignee and assigned_at envelope timestamp.
// Why (Phase J Path B): @mention reassignment must bump assigned_at to the trigger message's envelope
// timestamp. Splitting into two separate updates is racy and leaves stale assigned_at on intermediate reads.
func UpdateTaskAssigneeAndAssignedAt(ctx context.Context, q Querier, email string, id MessageID, assignee string, assignedAt time.Time) error {
	return withTx(ctx, q, func(qw Querier) error {
		if err := db.New(qw).UpdateTaskAssigneeAndAssignedAt(ctx, db.UpdateTaskAssigneeAndAssignedAtParams{
			ID:         int64(id),
			UserEmail:  nullString(email),
			Assignee:   nullString(assignee),
			AssignedAt: sql.NullTime{Time: assignedAt, Valid: !assignedAt.IsZero()},
		}); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

// UpdateTaskAssigneesBatch updates multiple tasks' assignees in a single transaction.
// Why: [Performance] Eliminates N+1 DB operations by batching updates and invalidating cache once.
func UpdateTaskAssigneesBatch(ctx context.Context, email string, updates map[MessageID]string) error {
	if len(updates) == 0 {
		return nil
	}

	err := RunInTx(ctx, func(tx *sql.Tx) error {
		queries := db.New(tx)
		for id, assignee := range updates {
			err := queries.UpdateMessageDetails(ctx, db.UpdateMessageDetailsParams{
				Assignee:  nullString(assignee),
				ID:        int64(id),
				UserEmail: nullString(email),
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil {
		InvalidateCacheActive(email)
	}
	return err
}

func UpdateTaskFullAppend(ctx context.Context, q Querier, email, room string, id MessageID, newTask, newOriginalText string) error {
	// Why: An empty newTask would overwrite the existing title and hide the row.
	// Fall back to append-only path so original_text still grows for audit while
	// task text is preserved.
	if strings.TrimSpace(newTask) == "" {
		logger.Warnf("[STORE] UpdateTaskFullAppend: empty newTask for id=%d, falling back to append-only", id)
		return AppendOriginalText(ctx, q, email, room, id, newOriginalText)
	}
	err := db.New(q).UpdateTaskFullAppend(ctx, db.UpdateTaskFullAppendParams{
		Task:         nullString(newTask),
		OriginalText: nullString(newOriginalText),
		ID:           int64(id),
		UserEmail:    nullString(email),
		Room:         nullString(room),
	})
	if err == nil {
		InvalidateCacheActive(email)
		InvalidateTaskTranslation(ctx, q, id)
	}
	return err
}

func AppendOriginalText(ctx context.Context, q Querier, email, room string, id MessageID, text string) error {
	err := db.New(q).AppendOriginalText(ctx, db.AppendOriginalTextParams{
		OriginalText: nullString(text),
		ID:           int64(id),
		UserEmail:    nullString(email),
		Room:         nullString(room),
	})
	if err == nil {
		InvalidateCacheActive(email)
	}
	return err
}

func UpdateTaskSourceChannels(ctx context.Context, q Querier, email string, id MessageID, channels []string) error {
	channelsJSON, _ := json.Marshal(channels)
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.SourceChannels = nullString(string(channelsJSON))
	})
}

func batchMsgOp(email string, ids []MessageID, op func([]int64) error) error {
	if len(ids) == 0 {
		return nil
	}
	if err := op(toInt64List(ids)); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func DeleteMessages(ctx context.Context, q Querier, email string, ids []MessageID) error {
	return batchMsgOp(email, ids, func(i64 []int64) error {
		return db.New(q).DeleteMessages(ctx, db.DeleteMessagesParams{UserEmail: nullString(email), Ids: i64})
	})
}

func HardDeleteMessages(ctx context.Context, q Querier, email string, ids []MessageID) error {
	return batchMsgOp(email, ids, func(i64 []int64) error {
		return db.New(q).HardDeleteMessages(ctx, db.HardDeleteMessagesParams{UserEmail: nullString(email), Ids: i64})
	})
}

func RestoreMessages(ctx context.Context, q Querier, email string, ids []MessageID) error {
	return batchMsgOp(email, ids, func(i64 []int64) error {
		return db.New(q).RestoreMessages(ctx, db.RestoreMessagesParams{UserEmail: nullString(email), Ids: i64})
	})
}
