package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/logger"
	"strings"
	"time"
)

func executeUpdateMessageDetails(ctx context.Context, q Querier, email string, id MessageID, updateFn func(*db.UpdateMessageDetailsParams)) error {
	params := db.UpdateMessageDetailsParams{ID: int64(id), UserEmail: nullString(email)}
	updateFn(&params)
	return updateMessageWithTitleInvalidation(ctx, q, email, id, params.Task, func(qw Querier) error {
		return db.New(qw).UpdateMessageDetails(ctx, params)
	})
}

// Why: ErrNoRows means the message vanished; callers skip silently instead of failing.
func getMessageForUpdate(ctx context.Context, qw Querier, id MessageID) (db.GetMessageByIDRow, bool, error) {
	row, err := db.New(qw).GetMessageByID(ctx, int64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("read title before update: %w", err)
	}
	return row, true, nil
}

// Why: skip=true when the row is gone or owned by another user, so the update must not run.
func checkTitleChange(ctx context.Context, qw Querier, email string, id MessageID, title sql.NullString) (changed, skip bool, err error) {
	if !title.Valid {
		return false, false, nil
	}
	row, found, err := getMessageForUpdate(ctx, qw, id)
	if err != nil {
		return false, false, err
	}
	if !found || row.UserEmail != email {
		return false, true, nil
	}
	return row.Task != title.String, false, nil
}

func updateMessageWithTitleInvalidation(ctx context.Context, q Querier, email string, id MessageID, title sql.NullString, update func(Querier) error) error {
	return withMessageTitleTx(ctx, q, func(qw Querier) error {
		changed, skip, err := checkTitleChange(ctx, qw, email, id, title)
		if err != nil {
			return err
		}
		if skip {
			return nil
		}
		if err := update(qw); err != nil {
			return fmt.Errorf("update message: %w", err)
		}
		if changed {
			if err := invalidateTaskTranslation(ctx, qw, id); err != nil {
				return err
			}
		}
		InvalidateCache(email)
		return nil
	})
}

func withMessageTitleTx(ctx context.Context, q Querier, update func(Querier) error) error {
	if q == nil {
		return RunInTx(ctx, func(tx *sql.Tx) error { return update(tx) })
	}
	conn, ok := q.(*sql.DB)
	if !ok {
		return update(q)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin message title transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Why: Deferred rollback safety net; the explicit Commit overrides on success.
	if err := update(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit message title transaction: %w", err)
	}
	return nil
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

// PendingCompletionCandidate is an open task carrying an unreviewed confirm-first completion suggestion.
type PendingCompletionCandidate struct {
	ID         MessageID
	Task       string
	Room       string
	Source     string
	SourceText string
	DetectedAt string
}

// ListPendingCompletionCandidates returns open tasks with a pending completion candidate, oldest detection first.
func ListPendingCompletionCandidates(ctx context.Context, email string, limit int) ([]PendingCompletionCandidate, error) {
	rows, err := db.New(GetDB()).ListPendingCompletionCandidates(ctx, db.ListPendingCompletionCandidatesParams{
		UserEmail: nullString(email), RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list pending completion candidates: %w", err)
	}
	out := make([]PendingCompletionCandidate, 0, len(rows))
	for _, r := range rows {
		out = append(out, PendingCompletionCandidate{
			ID: MessageID(r.ID), Task: r.Task, Room: r.Room, Source: r.Source,
			SourceText: r.SourceText, DetectedAt: r.DetectedAt,
		})
	}
	return out, nil
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

// Why: shared by markMessageDoneTrue and the subtask auto-close so both keep identical side effects.
const markDoneSetSQL = `UPDATE messages
	SET done = 1, completed_at = ?,
	    metadata = json_remove(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `'),
	    confirmed_at = COALESCE(confirmed_at, CURRENT_TIMESTAMP),
	    updated_at = CURRENT_TIMESTAMP`

// markMessageDoneTrue sets done+completed_at and clears any pending completion
// candidate in one statement — Why: raw SQL (precedent: unmarkMessageDone) since
// UpdateMessageDetails cannot express the json_remove; a task explicitly marked
// done should not keep surfacing a stale confirm-first suggestion.
func markMessageDoneTrue(ctx context.Context, q Querier, email string, id MessageID) error {
	return withTx(ctx, q, func(qw Querier) error {
		const stmt = markDoneSetSQL + ` WHERE id = ? AND user_email = ?`
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
			    metadata = json_remove(COALESCE(NULLIF(metadata, ''), '{}'), '$.` + metaKeyCompletionCandidate + `'),
			    confirmed_at = COALESCE(confirmed_at, CURRENT_TIMESTAMP)
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
	return updateMessageWithTitleInvalidation(ctx, q, email, id, f.Task, func(qw Querier) error {
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

// Why: one json_set statement avoids lost concurrent toggles; json('true') because a bound bool is stored as 1, not a JSON boolean.
const setSubtaskDoneSQL = `UPDATE messages
	SET subtasks = json_set(subtasks, '$[' || ? || '].done', CASE WHEN ? THEN json('true') ELSE json('false') END)
	WHERE id = ? AND user_email = ? AND ? BETWEEN 0 AND json_array_length(subtasks) - 1`

// Why: conditional close in one statement so the all-done check sees every committed concurrent toggle.
const closeParentIfAllSubtasksDoneSQL = markDoneSetSQL + `
	WHERE id = ? AND user_email = ? AND done = 0 AND json_array_length(subtasks) > 0
	  AND NOT EXISTS (SELECT 1 FROM json_each(subtasks) WHERE json_extract(value, '$.done') IS NOT 1)`

// UpdateSubtaskStatus toggles the 'done' status of a specific subtask and auto-closes the parent when the last one is checked.
func UpdateSubtaskStatus(ctx context.Context, q Querier, email string, id MessageID, subtaskIndex int, done bool) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}

	res, err := q.ExecContext(ctx, setSubtaskDoneSQL, subtaskIndex, done, int64(id), email, subtaskIndex)
	if err != nil {
		return fmt.Errorf("failed to update subtasks in DB: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to read subtask update result: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("subtask %d of task %d not found or not owned", subtaskIndex, id)
	}

	if done && closeParentIfAllSubtasksDone(ctx, q, email, id) {
		InvalidateCache(email)
		return nil
	}
	InvalidateCacheActive(email)
	return nil
}

func closeParentIfAllSubtasksDone(ctx context.Context, q Querier, email string, id MessageID) bool {
	res, err := q.ExecContext(ctx, closeParentIfAllSubtasksDoneSQL, time.Now(), int64(id), email)
	if err != nil {
		logger.Warnf("[STORE] subtask auto-close parent %d: %v", id, err)
		return false
	}
	n, err := res.RowsAffected()
	if err != nil {
		logger.Warnf("[STORE] subtask auto-close parent %d rows affected: %v", id, err)
		return true // Why: outcome unknown; treating it as closed invalidates both caches, the safe side.
	}
	return n > 0
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

// Why: an empty title would blank the task, so degrade to append-only.
func appendOriginalTextOnly(ctx context.Context, q Querier, email, room string, id MessageID, newOriginalText string) error {
	logger.Warnf("[STORE] UpdateTaskFullAppend: empty newTask for id=%d, falling back to append-only", id)
	if q == nil {
		q = GetDB()
	}
	return AppendOriginalText(ctx, q, email, room, id, newOriginalText)
}

func UpdateTaskFullAppend(ctx context.Context, q Querier, email, room string, id MessageID, newTask, newOriginalText string) error {
	if strings.TrimSpace(newTask) == "" {
		return appendOriginalTextOnly(ctx, q, email, room, id, newOriginalText)
	}
	updated := false
	err := withMessageTitleTx(ctx, q, func(qw Querier) error {
		row, found, err := getMessageForUpdate(ctx, qw, id)
		if err != nil {
			return err
		}
		if !found || row.UserEmail != email || row.Room != room {
			return nil
		}
		if err := db.New(qw).UpdateTaskFullAppend(ctx, db.UpdateTaskFullAppendParams{
			Task:         nullString(newTask),
			OriginalText: nullString(newOriginalText),
			ID:           int64(id),
			UserEmail:    nullString(email),
			Room:         nullString(room),
		}); err != nil {
			return fmt.Errorf("update message: %w", err)
		}
		if row.Task != newTask {
			if err := invalidateTaskTranslation(ctx, qw, id); err != nil {
				return err
			}
		}
		updated = true
		return nil
	})
	if err == nil && updated {
		InvalidateCacheActive(email)
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

// RenameRoom rewrites the stored room label for one channel room, returning how many rows
// moved. Why: a room whose display name only becomes resolvable later (a WhatsApp @lid chat,
// a group whose subject fetch failed) leaves history under the old label, and reports would
// then count one conversation as two rooms.
func RenameRoom(ctx context.Context, email, source, oldRoom, newRoom string) (int64, error) {
	if email == "" || source == "" || oldRoom == "" || newRoom == "" || oldRoom == newRoom {
		return 0, nil
	}
	return db.New(GetDB()).RenameRoom(ctx, db.RenameRoomParams{
		Room:      nullString(newRoom),
		UserEmail: nullString(email),
		Source:    nullString(source),
		Room_2:    nullString(oldRoom),
	})
}
