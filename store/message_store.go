package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"message-consolidator/db"
	"strings"
	"time"
)

// SaveMessage persists a single message and updates the local cache.
// Why: Enforces 30-line limit by delegating duplication checks, DB insertion, and cache synchronization to specific helpers.
// SaveMessage persists a single message and updates the local cache. Supports transactions.
func SaveMessage(ctx context.Context, q Querier, msg ConsolidatedMessage) (bool, int, error) {
	if isDuplicate(msg.UserEmail, msg.SourceTS) {
		return false, 0, nil
	}

	msg.Requester = NormalizeName(msg.UserEmail, msg.Requester)
	msg.Assignee = NormalizeName(msg.UserEmail, msg.Assignee)

	lastID, err := db.New(q).CreateMessage(ctx, toCreateMessageParams(msg))
	if err != nil {
		if err == sql.ErrNoRows {
			return false, 0, nil
		}
		return false, int(lastID), err
	}
	if lastID == 0 {
		return false, 0, nil
	}

	InvalidateCache(msg.UserEmail)
	return true, int(lastID), nil
}

func isDuplicate(email, ts string) bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	userKnown, ok := knownTS[email]
	return ok && userKnown[ts]
}

// IsProcessed checks if a message has already been handled by checking both cache and DB.
// Why: Ensures idempotency even across restarts/cache misses by performing a final DB-level verification.
func IsProcessed(ctx context.Context, q Querier, email, sourceTS string) (bool, error) {
	if isDuplicate(email, sourceTS) {
		return true, nil
	}

	queries := db.New(q)
	count, err := queries.IsMessageProcessed(ctx, db.IsMessageProcessedParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		SourceTs:  sql.NullString{String: sourceTS, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("failed to check if message is processed: %w", err)
	}
	// count is the result of IsMessageProcessed (int64)
	return count > 0, nil
}

// MarkAsProcessed manually registers a SourceTS as processed to prevent redundant AI extraction.
// Why: [Early Return] Allows the scanner to skip standard extraction for messages handled via the completion pipeline.
func MarkAsProcessed(ctx context.Context, q Querier, email, sourceTS string) error {
	cacheMu.Lock()
	if _, ok := knownTS[email]; !ok {
		knownTS[email] = make(map[string]bool)
	}
	knownTS[email][sourceTS] = true
	cacheMu.Unlock()

	return db.New(q).UpdateProcessed(ctx, db.UpdateProcessedParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		SourceTs:  sql.NullString{String: sourceTS, Valid: true},
	})
}

// SaveMessages performs a bulk insert of multiple messages.
// Why: Refactored to satisfy 30-line limit by delegating bulk preparation, DB execution, and multi-user cache updates.
func SaveMessages(ctx context.Context, msgs []ConsolidatedMessage) ([]int, error) {
	toInsert := filterNewOnly(msgs)
	if len(toInsert) == 0 {
		return nil, nil
	}

	normalizeMsgs(toInsert)
	newIDsMap, err := executeBulkInsert(ctx, toInsert)
	if err != nil {
		return nil, err
	}

	// Why: Batch invalidation for all users affected by the bulk operation.
	for email := range newIDsMap {
		InvalidateCache(email)
	}
	
	return flattenIDs(newIDsMap), nil
}

func flattenIDs(newIDsMap map[string]map[string]int) []int {
	var ids []int
	for _, userMap := range newIDsMap {
		for _, id := range userMap {
			ids = append(ids, id)
		}
	}
	return ids
}

func filterNewOnly(msgs []ConsolidatedMessage) []ConsolidatedMessage {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	var filtered []ConsolidatedMessage
	for _, m := range msgs {
		if known, ok := knownTS[m.UserEmail]; !ok || !known[m.SourceTS] {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func normalizeMsgs(msgs []ConsolidatedMessage) {
	for i := range msgs {
		msgs[i].Requester = NormalizeName(msgs[i].UserEmail, msgs[i].Requester)
		msgs[i].Assignee = NormalizeName(msgs[i].UserEmail, msgs[i].Assignee)
	}
}

func executeBulkInsert(ctx context.Context, msgs []ConsolidatedMessage) (map[string]map[string]int, error) {
	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, LogSQLError("BeginTx (BulkInsert)", err)
	}
	defer tx.Rollback()

	queries := db.New(tx)
	res := make(map[string]map[string]int)

	for _, msg := range msgs {
		id, err := queries.CreateMessage(ctx, toCreateMessageParams(msg))
		if err != nil {
			return nil, LogSQLError("CreateMessage (BulkInsert)", err, msg.UserEmail, msg.SourceTS)
		}
		if res[msg.UserEmail] == nil {
			res[msg.UserEmail] = make(map[string]int)
		}
		res[msg.UserEmail][msg.SourceTS] = int(id)
	}

	if err := tx.Commit(); err != nil {
		return nil, LogSQLError("Commit (BulkInsert)", err)
	}
	return res, nil
}

// scanBulkIDs is deprecated after refactoring to transaction-based CreateMessage loop.

func GetMessages(ctx context.Context, email string) ([]ConsolidatedMessage, error) {
	if err := EnsureCacheInitialized(ctx, email); err != nil {
		return nil, err
	}

	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if msgs, ok := messageCache[email]; ok {
		return msgs, nil
	}
	return []ConsolidatedMessage{}, nil
}

func MarkMessageDone(ctx context.Context, q Querier, email string, id int, done bool) error {
	if q == nil {
		return RunInTx(ctx, func(tx *sql.Tx) error {
			return markMessageDoneInternal(ctx, tx, email, id, done)
		})
	}
	return markMessageDoneInternal(ctx, q, email, id, done)
}

func markMessageDoneInternal(ctx context.Context, q Querier, email string, id int, done bool) error {
	var comp sql.NullTime
	if done {
		comp = sql.NullTime{Time: time.Now(), Valid: true}
	}

	err := db.New(q).MarkMessageDone(ctx, db.MarkMessageDoneParams{
		Done:        sql.NullBool{Bool: done, Valid: true},
		CompletedAt: comp,
		ID:          int64(id),
		UserEmail:   sql.NullString{String: email, Valid: true},
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func UpdateTaskText(ctx context.Context, q Querier, email string, id int, task string) error {
	if q == nil {
		return RunInTx(ctx, func(tx *sql.Tx) error {
			return updateTaskTextInternal(ctx, tx, email, id, task)
		})
	}
	return updateTaskTextInternal(ctx, q, email, id, task)
}

func updateTaskTextInternal(ctx context.Context, q Querier, email string, id int, task string) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}
	err := db.New(q).UpdateTaskText(ctx, db.UpdateTaskTextParams{
		Task:      sql.NullString{String: task, Valid: true},
		ID:        int64(id),
		UserEmail: sql.NullString{String: email, Valid: true},
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

// UpdateTaskDescriptionAppend appends new content to the task text only.
// Why: [Context Isolation] Requires user_email and room to prevent cross-room data manipulation. Supports transactions.
func UpdateTaskDescriptionAppend(ctx context.Context, q Querier, email, room string, id int, date, newTask string) error {
	return db.New(q).UpdateTaskDescriptionAppend(ctx, db.UpdateTaskDescriptionAppendParams{
		Task:      sql.NullString{String: date, Valid: true},
		Task_2:    sql.NullString{String: newTask, Valid: true},
		ID:        int64(id),
		UserEmail: sql.NullString{String: email, Valid: true},
		Room:      sql.NullString{String: room, Valid: true},
	})
}

// UpdateTaskFullAppend appends new content to both task and original_text.
// Why: [Context Isolation] Requires user_email and room to ensure updates apply only to the correct context.
func UpdateTaskFullAppend(ctx context.Context, q Querier, email, room string, id int, date, newTask, newOriginalText string) error {
	err := db.New(q).UpdateTaskFullAppend(ctx, db.UpdateTaskFullAppendParams{
		Task:         sql.NullString{String: date, Valid: true},
		Task_2:       sql.NullString{String: newTask, Valid: true},
		OriginalText: sql.NullString{String: newOriginalText, Valid: true},
		ID:           int64(id),
		UserEmail:    sql.NullString{String: email, Valid: true},
		Room:         sql.NullString{String: room, Valid: true},
	})
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

// MergeTasks consolidates multiple tasks into one.
// Why: Uses a single transaction and strings.Builder to maintain data integrity and memory efficiency during large text concatenation.
func MergeTasks(ctx context.Context, email string, targetIDs []int, destID int) error {
	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := executeMerge(ctx, tx, email, targetIDs, destID); err != nil {
		return err
	}

	InvalidateCache(email)
	return nil
}

// MergeTasksWithTitle consolidates multiple tasks into one with a specific title (AI generated).
// Why: [Unified Consolidation] Combines source tasks into a destination task while setting a new optimized title.
func MergeTasksWithTitle(ctx context.Context, email string, targetIDs []int64, destID int64, newTitle string) error {
	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	allIDs := append(toIntList(targetIDs), int(destID))
	msgs, err := GetMessagesByIDs(ctx, tx, email, allIDs)
	if err != nil {
		return err
	}

	dest, sources, err := splitMergeTasks(msgs, destID)
	if err != nil {
		return err
	}

	history := buildMergeHistory(dest.Task, sources)
	if err := applyMergeTransaction(ctx, tx, email, dest.Room, targetIDs, dest.ID, newTitle, history); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func toInt64List(ids []int) []int64 {
	res := make([]int64, len(ids))
	for i, id := range ids { res[i] = int64(id) }
	return res
}

func toIntList(ids []int64) []int {
	res := make([]int, len(ids))
	for i, id := range ids { res[i] = int(id) }
	return res
}

func splitMergeTasks(msgs []ConsolidatedMessage, destID int64) (*ConsolidatedMessage, []ConsolidatedMessage, error) {
	var dest *ConsolidatedMessage
	var sources []ConsolidatedMessage
	for i := range msgs {
		if int64(msgs[i].ID) == destID {
			dest = &msgs[i]
		} else {
			sources = append(sources, msgs[i])
		}
	}
	if dest == nil { return nil, nil, fmt.Errorf("destination task %d not found", destID) }
	return dest, sources, nil
}

func buildMergeHistory(oldTitle string, sources []ConsolidatedMessage) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("\n\n--- [Merge History] ---\nPrev Title: %s\n", oldTitle))
	for _, s := range sources {
		builder.WriteString(fmt.Sprintf("\n--- [Source: %d] ---\nTitle: %s\nText: %s\n", s.ID, s.Task, s.OriginalText))
	}
	return builder.String()
}

func applyMergeTransaction(ctx context.Context, tx *sql.Tx, email, room string, targetIDs []int64, destID int, title, history string) error {
	queries := db.New(tx)
	if err := queries.UpdateTaskMergeComplete(ctx, db.UpdateTaskMergeCompleteParams{
		Task:         sql.NullString{String: title, Valid: true},
		OriginalText: sql.NullString{String: history, Valid: true},
		ID:           int64(destID),
		UserEmail:    sql.NullString{String: email, Valid: true},
		Room:         sql.NullString{String: room, Valid: true},
	}); err != nil {
		return err
	}

	targetIDsInt := make([]int, len(targetIDs))
	for i, id := range targetIDs {
		targetIDsInt[i] = int(id)
	}

	if err := queries.UpdateCategoryMerged(ctx, db.UpdateCategoryMergedParams{
		Ids:       targetIDs,
		UserEmail: sql.NullString{String: email, Valid: true},
	}); err != nil {
		return err
	}

	// Why: Ensures all merged tasks (sources and destination) clear their translation cache to prevent stale text.
	allIDs := append(targetIDsInt, destID)
	for _, id := range allIDs {
		if err := queries.DeleteTaskTranslations(ctx, sql.NullInt64{Int64: int64(id), Valid: true}); err != nil {
			return err
		}
	}
	return nil
}

func executeMerge(ctx context.Context, tx *sql.Tx, email string, targets []int, destID int) error {
	allIDs := append(targets, destID)
	msgs, err := GetMessagesByIDs(ctx, tx, email, allIDs)
	if err != nil {
		return err
	}

	var dest *ConsolidatedMessage
	var sources []ConsolidatedMessage
	for i := range msgs {
		if msgs[i].ID == destID {
			dest = &msgs[i]
		} else {
			sources = append(sources, msgs[i])
		}
	}

	if dest == nil || len(sources) == 0 {
		return fmt.Errorf("invalid merge: destination or sources not found")
	}

	return applyMergeUpdates(ctx, tx, email, dest.Room, dest, sources, targets)
}

func applyMergeUpdates(ctx context.Context, tx *sql.Tx, email, room string, dest *ConsolidatedMessage, sources []ConsolidatedMessage, targets []int) error {
	var taskBuilder, textBuilder strings.Builder

	for i, s := range sources {
		if i > 0 {
			taskBuilder.WriteString("\n\n")
			textBuilder.WriteString("\n\n")
		}
		divider := fmt.Sprintf("=== [Merged Task: %d] ===\n", s.ID)
		taskBuilder.WriteString(divider + s.Task)
		textBuilder.WriteString(divider + s.OriginalText)
	}

	queries := db.New(tx)
	err := queries.UpdateTaskFullAppend(ctx, db.UpdateTaskFullAppendParams{
		Task:         sql.NullString{String: "Manual Merge", Valid: true},
		Task_2:       sql.NullString{String: taskBuilder.String(), Valid: true},
		OriginalText: sql.NullString{String: textBuilder.String(), Valid: true},
		ID:           int64(dest.ID),
		UserEmail:    sql.NullString{String: email, Valid: true},
		Room:         sql.NullString{String: room, Valid: true},
	})
	if err != nil {
		return err
	}

	return queries.UpdateCategoryMerged(ctx, db.UpdateCategoryMergedParams{
		Ids:       toInt64List(targets),
		UserEmail: sql.NullString{String: email, Valid: true},
	})
}

func UpdateMessageCategory(ctx context.Context, q Querier, email string, id int, category string) error {
	if q == nil {
		return RunInTx(ctx, func(tx *sql.Tx) error {
			return updateMessageCategoryInternal(ctx, tx, email, id, category)
		})
	}
	return updateMessageCategoryInternal(ctx, q, email, id, category)
}

func updateMessageCategoryInternal(ctx context.Context, q Querier, email string, id int, category string) error {
	err := db.New(q).UpdateMessageCategory(ctx, db.UpdateMessageCategoryParams{
		Category:  sql.NullString{String: category, Valid: true},
		ID:        int64(id),
		UserEmail: sql.NullString{String: email, Valid: true},
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func UpdateTaskAssignee(ctx context.Context, q Querier, email string, id int, assignee string) error {
	if q == nil {
		return RunInTx(ctx, func(tx *sql.Tx) error {
			return updateTaskAssigneeInternal(ctx, tx, email, id, assignee)
		})
	}
	return updateTaskAssigneeInternal(ctx, q, email, id, assignee)
}

func updateTaskAssigneeInternal(ctx context.Context, q Querier, email string, id int, assignee string) error {
	err := db.New(q).UpdateTaskAssignee(ctx, db.UpdateTaskAssigneeParams{
		Assignee:  sql.NullString{String: assignee, Valid: true},
		ID:        int64(id),
		UserEmail: sql.NullString{String: email, Valid: true},
	})
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

// UpdateTaskAssigneesBatch updates multiple tasks' assignees in a single transaction.
// Why: [Performance] Eliminates N+1 DB operations by batching updates and invalidating cache once.
func UpdateTaskAssigneesBatch(ctx context.Context, email string, updates map[int]string) error {
	if len(updates) == 0 {
		return nil
	}

	err := RunInTx(ctx, func(tx *sql.Tx) error {
		queries := db.New(tx)
		for id, assignee := range updates {
			err := queries.UpdateTaskAssignee(ctx, db.UpdateTaskAssigneeParams{
				Assignee:  sql.NullString{String: assignee, Valid: true},
				ID:        int64(id),
				UserEmail: sql.NullString{String: email, Valid: true},
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil {
		InvalidateCache(email)
	}
	return err
}

// UpdateMessageIdentity updates both requester and assignee for a task.
// Why: [Security] Uses composite key (ID, UserEmail, Room) to prevent cross-account/room modifications. Supports transactions.
func UpdateMessageIdentity(ctx context.Context, q Querier, email, room string, id int, requester, assignee string) error {
	err := db.New(q).UpdateMessageIdentity(ctx, db.UpdateMessageIdentityParams{
		Requester: sql.NullString{String: requester, Valid: true},
		Assignee:  sql.NullString{String: assignee, Valid: true},
		ID:        int64(id),
		UserEmail: sql.NullString{String: email, Valid: true},
		Room:      sql.NullString{String: room, Valid: true},
	})
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func UpdateTaskSourceChannels(ctx context.Context, q Querier, email string, id int, channels []string) error {
	channelsJSON, _ := json.Marshal(channels)
	err := db.New(q).UpdateTaskSourceChannels(ctx, db.UpdateTaskSourceChannelsParams{
		SourceChannels: sql.NullString{String: string(channelsJSON), Valid: true},
		ID:             int64(id),
		UserEmail:      sql.NullString{String: email, Valid: true},
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func DeleteMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.New(q).DeleteMessages(ctx, db.DeleteMessagesParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		Ids:       toInt64List(ids),
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func HardDeleteMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.New(q).HardDeleteMessages(ctx, db.HardDeleteMessagesParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		Ids:       toInt64List(ids),
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func RestoreMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.New(q).RestoreMessages(ctx, db.RestoreMessagesParams{
		UserEmail: sql.NullString{String: email, Valid: true},
		Ids:       toInt64List(ids),
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func GetMessageByID(ctx context.Context, q Querier, email string, id int) (ConsolidatedMessage, error) {
	if msg, found := findMessageInCache(email, id); found {
		return msg, nil
	}
	if q == nil {
		q = GetDB()
	}
	// Why: Fallback to database only if not found in active or recently archived caches.
	row, err := db.New(q).GetMessageByID(ctx, int64(id))
	if err != nil {
		return ConsolidatedMessage{}, err
	}
	return toConsolidatedFromByID(row), nil
}

func findMessageInCache(email string, id int) (ConsolidatedMessage, bool) {
	if email == "" { return ConsolidatedMessage{}, false }
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	
	userCache := messageCache[email]
	for _, m := range userCache {
		if m.ID == id { return m, true }
	}
	
	userArchive := archiveCache[email]
	for _, m := range userArchive {
		if m.ID == id { return m, true }
	}
	return ConsolidatedMessage{}, false
}

func GetMessagesByIDs(ctx context.Context, q Querier, email string, ids []int) ([]ConsolidatedMessage, error) {
	if len(ids) == 0 {
		return []ConsolidatedMessage{}, nil
	}

	found, missing := extractFromCache(email, ids)
	if len(missing) == 0 {
		return found, nil
	}

	rows, err := db.New(q).GetMessagesByIDs(ctx, toInt64List(missing))
	if err != nil {
		return nil, LogSQLError("GetMessagesByIDs", err, missing)
	}

	fromDB := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		fromDB[i] = toConsolidatedFromByIDs(row)
	}
	return append(found, fromDB...), nil
}

func extractFromCache(email string, ids []int) ([]ConsolidatedMessage, []int) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	var found []ConsolidatedMessage
	var missing []int
	for _, id := range ids {
		if m, ok := searchCache(email, id); ok {
			found = append(found, m)
		} else {
			missing = append(missing, id)
		}
	}
	return found, missing
}

func searchCache(email string, id int) (ConsolidatedMessage, bool) {
	for _, m := range messageCache[email] {
		if m.ID == id { return m, true }
	}
	for _, m := range archiveCache[email] {
		if m.ID == id { return m, true }
	}
	return ConsolidatedMessage{}, false
}

func GetIncompleteByThreadID(ctx context.Context, q Querier, email, threadID string) ([]ConsolidatedMessage, error) {
	if threadID == "" {
		return []ConsolidatedMessage{}, nil
	}
	rows, err := db.New(q).GetIncompleteByThreadID(ctx, db.GetIncompleteByThreadIDParams{
		UserEmail: email,
		ThreadID:  threadID,
	})
	if err != nil {
		return nil, err
	}

	msgs := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		msgs[i] = toConsolidatedFromIncomplete(row)
	}
	return msgs, nil
}

// GetActiveContextTasks retrieves a subset of incomplete tasks to provide context for AI analysis.
// Why: Limits results to 50 items and 30 days to optimize AI token usage and memory overhead.
func GetActiveContextTasks(ctx context.Context, q Querier, email, source, room string) ([]ConsolidatedMessage, error) {
	rows, err := db.New(q).GetActiveTasksForContext(ctx, db.GetActiveTasksForContextParams{
		UserEmail: email,
		Source:    source,
		Room:      room,
	})
	if err != nil {
		return nil, LogSQLError("GetActiveTasksForContext", err, email, source, room)
	}

	msgs := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		msgs[i] = toConsolidatedFromContext(row)
	}
	return msgs, nil
}

func toCreateMessageParams(msg ConsolidatedMessage) db.CreateMessageParams {
	constraintsJSON, _ := json.Marshal(msg.Constraints)
	channelsJSON, _ := json.Marshal(msg.SourceChannels)
	contextJSON, _ := json.Marshal(msg.ConsolidatedContext)
	isCtx := 0
	if msg.IsContextQuery {
		isCtx = 1
	}

	params := db.CreateMessageParams{
		UserEmail:           sql.NullString{String: msg.UserEmail, Valid: true},
		Source:              sql.NullString{String: msg.Source, Valid: true},
		Room:                sql.NullString{String: msg.Room, Valid: true},
		Task:                sql.NullString{String: msg.Task, Valid: true},
		Requester:           sql.NullString{String: msg.Requester, Valid: true},
		Assignee:            sql.NullString{String: msg.Assignee, Valid: true},
		AssignedAt:          sql.NullTime{Time: msg.AssignedAt, Valid: !msg.AssignedAt.IsZero()},
		Link:                sql.NullString{String: msg.Link, Valid: true},
		SourceTs:            sql.NullString{String: msg.SourceTS, Valid: true},
		OriginalText:        sql.NullString{String: msg.OriginalText, Valid: true},
		Category:            sql.NullString{String: msg.Category, Valid: true},
		Deadline:            sql.NullString{String: msg.Deadline, Valid: true},
		ThreadID:            sql.NullString{String: msg.ThreadID, Valid: true},
		AssigneeReason:      sql.NullString{String: msg.AssigneeReason, Valid: true},
		RepliedToID:         sql.NullString{String: msg.RepliedToID, Valid: true},
		IsContextQuery:      sql.NullInt64{Int64: int64(isCtx), Valid: true},
		Constraints:         sql.NullString{String: string(constraintsJSON), Valid: true},
		Metadata:            sql.NullString{String: string(msg.Metadata), Valid: true},
		SourceChannels:      sql.NullString{String: string(channelsJSON), Valid: true},
		ConsolidatedContext: sql.NullString{String: string(contextJSON), Valid: true},
		Subtasks:            sql.NullString{String: encodeSubtasks(msg.Subtasks), Valid: true},
	}

	return params
}

func encodeSubtasks(subtasks []Subtask) string {
	if len(subtasks) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(subtasks)
	return string(data)
}


func toConsolidatedFromByID(row db.GetMessageByIDRow) ConsolidatedMessage {
	return mapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, "", "", row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}

func toConsolidatedFromByIDs(row db.GetMessagesByIDsRow) ConsolidatedMessage {
	return mapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, "", "", row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}

func toConsolidatedFromIncomplete(row db.GetIncompleteByThreadIDRow) ConsolidatedMessage {
	return mapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, "", "", row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}

func unmarshalMessageComponents(constraintsStr, channelsStr, contextStr, subtasksStr string) ([]string, []string, []string, []Subtask) {
	var constraints, channels, context []string
	var subtasks []Subtask
	_ = json.Unmarshal([]byte(constraintsStr), &constraints)
	_ = json.Unmarshal([]byte(channelsStr), &channels)
	_ = json.Unmarshal([]byte(contextStr), &context)
	_ = json.Unmarshal([]byte(subtasksStr), &subtasks)
	return constraints, channels, context, subtasks
}

func mapVMessageToConsolidated(
	id int, userEmail, source, room, task, requester, assignee, link, sourceTs,
	originalText string, done, isDeleted bool, createdAt sql.NullTime,
	category, deadline, threadID, reqCanonical, asgCanonical, asgReason,
	repliedToID string, isContextQuery int, constraintsStr, contextStr,
	metadataStr, channelsStr, reqType, asgType, reqDisp, asgDisp, subtasksStr string,
	assignedAt, completedAt sql.NullTime,
) ConsolidatedMessage {
	constraints, channels, context, subtasks := unmarshalMessageComponents(constraintsStr, channelsStr, contextStr, subtasksStr)

	msg := ConsolidatedMessage{
		ID: id, UserEmail: userEmail, Source: source, Room: room, Task: task,
		Requester: requester, Assignee: assignee, Link: link, SourceTS: sourceTs,
		OriginalText: originalText, Done: done, IsDeleted: isDeleted, CreatedAt: createdAt.Time,
		Category: category, Deadline: deadline, ThreadID: threadID,
		RequesterCanonical: reqCanonical, AssigneeCanonical: asgCanonical, AssigneeReason: asgReason,
		RepliedToID: repliedToID, IsContextQuery: isContextQuery > 0, Constraints: constraints,
		ConsolidatedContext: context, Metadata: json.RawMessage(metadataStr),
		SourceChannels: channels, RequesterType: reqType, AssigneeType: asgType, Subtasks: subtasks,
	}

	if assignedAt.Valid {
		msg.AssignedAt = assignedAt.Time
	}
	if completedAt.Valid {
		msg.CompletedAt = &completedAt.Time
	}
	return msg
}

func toConsolidatedFromContext(row db.GetActiveTasksForContextRow) ConsolidatedMessage {
	msg := ConsolidatedMessage{
		ID:           int(row.ID),
		Task:         row.Task,
		OriginalText: row.OriginalText,
		Requester:    row.Requester,
		Assignee:     row.Assignee,
		Source:       row.Source,
		Room:       row.Room,
		Done:         row.Done,
		Category:     row.Category,
		Subtasks:     []Subtask{},
	}

	if row.AssignedAt.Valid {
		msg.AssignedAt = row.AssignedAt.Time
	}
	if row.CompletedAt.Valid {
		msg.CompletedAt = &row.CompletedAt.Time
	}

	return msg
}

// CategorizeByUser groups a slice of messages into dashboard categories.
// Why: [SSOT] Unifies backend and frontend filtering logic into a single Go implementation.
func CategorizeByUser(msgs []ConsolidatedMessage, userEmail string, aliases []string) CategorizedMessages {
	res := CategorizedMessages{
		Inbox:   make([]ConsolidatedMessage, 0),
		Pending: make([]ConsolidatedMessage, 0),
		All:     msgs,
	}
	for _, m := range msgs {
		if IsAssignedToUser(m, userEmail, aliases) {
			res.Inbox = append(res.Inbox, m)
		} else {
			res.Pending = append(res.Pending, m)
		}
	}
	return res
}

func IsAssignedToUser(m ConsolidatedMessage, userEmail string, aliases []string) bool {
	// Explicitly ignore 'shared' tasks from individual inbox
	if strings.EqualFold(m.Assignee, "shared") {
		return false
	}
	// 1. Check Canonical ID (IDP SSOT)
	if strings.EqualFold(m.AssigneeCanonical, userEmail) {
		return true
	}

	// 2. Check Raw Assignee (Legacy/Fallback)
	assignee := m.Assignee
	if assignee == "me" || strings.EqualFold(assignee, userEmail) {
		return true
	}
	for _, a := range aliases {
		if strings.EqualFold(assignee, a) {
			return true
		}
	}
	return false
}
