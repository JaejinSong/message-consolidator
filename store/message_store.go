package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
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

	lastID, err := insertMessage(ctx, q, msg)
	if err != nil || lastID == 0 {
		return false, lastID, err
	}

	InvalidateCache(msg.UserEmail)
	return true, lastID, nil
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

	var processed bool
	err := q.QueryRowContext(ctx, SQL.IsMessageProcessed, email, sourceTS).Scan(&processed)
	if err != nil {
		return false, fmt.Errorf("failed to check if message is processed: %w", err)
	}
	return processed, nil
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
	// Why: [WhaTap-Memory] Bulk insert builds a large argument slice; 20 fields per message.
	valueStrings := make([]string, 0, len(msgs))
	valueArgs := make([]interface{}, 0, len(msgs)*20)

	for _, msg := range msgs {
		valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		constraintsJSON, _ := json.Marshal(msg.Constraints)
		channelsJSON, _ := json.Marshal(msg.SourceChannels)
		contextJSON, _ := json.Marshal(msg.ConsolidatedContext)
		valueArgs = append(valueArgs, 
			msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, 
			msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText, msg.Category, 
			msg.Deadline, msg.ThreadID, msg.AssigneeReason, msg.RepliedToID, 
		msg.IsContextQuery, string(constraintsJSON), string(msg.Metadata), string(channelsJSON),
		string(contextJSON),
		)
	}

	query := fmt.Sprintf(SQL.SaveMessagesBase, strings.Join(valueStrings, ","))
	rows, err := db.QueryContext(ctx, query, valueArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBulkIDs(rows)
}

func scanBulkIDs(rows *sql.Rows) (map[string]map[string]int, error) {
	res := make(map[string]map[string]int)
	for rows.Next() {
		var id int
		var ts, email string
		if err := rows.Scan(&id, &ts, &email); err == nil {
			if res[email] == nil {
				res[email] = make(map[string]int)
			}
			res[email][ts] = id
		}
	}
	return res, rows.Err()
}

func insertMessage(ctx context.Context, q Querier, msg ConsolidatedMessage) (int, error) {
	var lastID int
	constraintsJSON, _ := json.Marshal(msg.Constraints)
	channelsJSON, _ := json.Marshal(msg.SourceChannels)
	contextJSON, _ := json.Marshal(msg.ConsolidatedContext)
	
	err := q.QueryRowContext(ctx, SQL.SaveMessage,
		msg.UserEmail, msg.Source, msg.Room, msg.Task,
		msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link,
		msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline,
		msg.ThreadID, msg.AssigneeReason, msg.RepliedToID,
		msg.IsContextQuery, string(constraintsJSON), string(msg.Metadata),
		string(channelsJSON), string(contextJSON),
	).Scan(&lastID)

	if err != nil && err != sql.ErrNoRows {
		logger.Errorf("SaveMessage DB Scan Error: %v", err)
		return 0, err
	}
	return lastID, nil
}

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
	var comp interface{} = nil
	if done { comp = time.Now() }

	_, err := q.ExecContext(ctx, SQL.MarkMessageDone, done, comp, int(id), email)
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func UpdateTaskText(ctx context.Context, q Querier, email string, id int, task string) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}
	_, err := q.ExecContext(ctx, SQL.UpdateTaskText, task, int(id), email)
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

// UpdateTaskDescriptionAppend appends new content to the task text only.
// Why: [Context Isolation] Requires user_email and room to prevent cross-room data manipulation. Supports transactions.
func UpdateTaskDescriptionAppend(ctx context.Context, q Querier, email, room string, id int, date, newTask string) error {
	_, err := q.ExecContext(ctx, SQL.UpdateTaskDescriptionAppend, date, newTask, int(id), email, room)
	return err
}

// UpdateTaskFullAppend appends new content to both task and original_text.
// Why: [Context Isolation] Requires user_email and room to ensure updates apply only to the correct context.
func UpdateTaskFullAppend(ctx context.Context, q Querier, email, room string, id int, date, newTask, newOriginalText string) error {
	_, err := q.ExecContext(ctx, SQL.UpdateTaskFullAppend, date, newTask, newOriginalText, int(id), email, room)
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

// MergeTasks consolidates multiple tasks into one.
// Why: Uses a single transaction and strings.Builder to maintain data integrity and memory efficiency during large text concatenation.
func MergeTasks(ctx context.Context, email string, targetIDs []int, destID int) error {

	tx, err := db.BeginTx(ctx, nil)
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()

	allIDs := append(toIntList(targetIDs), int(destID))
	msgs, err := GetMessagesByIDs(ctx, tx, email, allIDs)
	if err != nil { return err }

	dest, sources, err := splitMergeTasks(msgs, destID)
	if err != nil { return err }

	history := buildMergeHistory(dest.Task, sources)
	if err := applyMergeTransaction(ctx, tx, email, dest.Room, targetIDs, dest.ID, newTitle, history); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil { return err }
	InvalidateCache(email)
	return nil
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
	if _, err := tx.ExecContext(ctx, SQL.UpdateTaskMergeComplete, title, history, destID, email, room); err != nil {
		return err
	}

	placeholders := strings.Repeat("?,", len(targetIDs)-1) + "?"
	query := fmt.Sprintf(SQL.UpdateCategoryMerged, placeholders)
	args := make([]interface{}, len(targetIDs)+1)
	for i, id := range targetIDs { args[i] = int(id) }
	args[len(targetIDs)] = email

	_, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	// Why: Ensures all merged tasks (sources and destination) clear their translation cache to prevent stale text.
	allIDs := append(targetIDs, int64(destID))
	for _, id := range allIDs {
		if _, err := tx.ExecContext(ctx, SQL.DeleteTaskTranslations, int(id)); err != nil {
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

	_, err := tx.ExecContext(ctx, SQL.UpdateTaskFullAppend, "Manual Merge", taskBuilder.String(), textBuilder.String(), dest.ID, email, room)
	if err != nil {
		return err
	}

	placeholders := strings.Repeat("?,", len(targets)-1) + "?"
	query := fmt.Sprintf(SQL.UpdateCategoryMerged, placeholders)
	args := make([]interface{}, len(targets)+1)
	for i, id := range targets {
		args[i] = int(id)
	}
	args[len(targets)] = email

	_, err = tx.ExecContext(ctx, query, args...)
	return err
}

func UpdateMessageCategory(ctx context.Context, q Querier, email string, id int, category string) error {
	_, err := q.ExecContext(ctx, SQL.UpdateMessageCategory, category, int(id), email)
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func UpdateTaskAssignee(ctx context.Context, q Querier, email string, id int, assignee string) error {
	_, err := q.ExecContext(ctx, SQL.UpdateTaskAssignee, assignee, int(id), email)
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

// UpdateMessageIdentity updates both requester and assignee for a task.
// Why: [Security] Uses composite key (ID, UserEmail, Room) to prevent cross-account/room modifications. Supports transactions.
func UpdateMessageIdentity(ctx context.Context, q Querier, email, room string, id int, requester, assignee string) error {
	_, err := q.ExecContext(ctx, SQL.UpdateMessageIdentity, requester, assignee, id, email, room)
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func UpdateMessageRequester(ctx context.Context, q Querier, email, room string, id int, requester string) error {
	query := "UPDATE messages SET requester = ? WHERE id = ? AND user_email = ? AND room = ?"
	_, err := q.ExecContext(ctx, query, requester, id, email, room)
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func UpdateMessageAssignee(ctx context.Context, q Querier, email, room string, id int, assignee string) error {
	query := "UPDATE messages SET assignee = ? WHERE id = ? AND user_email = ? AND room = ?"
	_, err := q.ExecContext(ctx, query, assignee, id, email, room)
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func UpdateTaskSourceChannels(ctx context.Context, q Querier, email string, id int, channels []string) error {
	channelsJSON, _ := json.Marshal(channels)
	_, err := q.ExecContext(ctx, SQL.UpdateTaskSourceChannels, string(channelsJSON), int(id), email)
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func DeleteMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (%s)", placeholders)
	args := prepareIDArgs(email, ids)
	
	_, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func HardDeleteMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("DELETE FROM messages WHERE user_email = ? AND id IN (%s)", placeholders)
	args := prepareIDArgs(email, ids)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func prepareIDArgs(email string, ids []int) []interface{} {
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = int(id)
	}
	return args
}

func RestoreMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("UPDATE messages SET is_deleted = 0, done = 0, completed_at = NULL WHERE user_email = ? AND id IN (%s)", placeholders)
	args := prepareIDArgs(email, ids)
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func GetMessageByID(ctx context.Context, q Querier, email string, id int) (ConsolidatedMessage, error) {
	if msg, found := findMessageInCache(email, id); found {
		return msg, nil
	}
	// Why: Fallback to database only if not found in active or recently archived caches.
	row := q.QueryRowContext(ctx, SQL.GetMessageByID, int(id), email)
	return scanMessageRow(row)
}

func findMessageInCache(email string, id int) (ConsolidatedMessage, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	for _, m := range messageCache[email] {
		if m.ID == id { return m, true }
	}
	for _, m := range archiveCache[email] {
		if m.ID == id { return m, true }
	}
	return ConsolidatedMessage{}, false
}

func GetMessagesByIDs(ctx context.Context, q Querier, email string, ids []int) ([]ConsolidatedMessage, error) {
	if len(ids) == 0 { return []ConsolidatedMessage{}, nil }

	found, missing := extractFromCache(email, ids)
	if len(missing) == 0 {
		return found, nil
	}

	fromDB, err := fetchMissingFromDB(ctx, q, email, missing)
	if err != nil {
		return nil, err
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

func fetchMissingFromDB(ctx context.Context, q Querier, email string, missing []int) ([]ConsolidatedMessage, error) {
	placeholders := strings.Repeat("?,", len(missing)-1) + "?"
	query := fmt.Sprintf("SELECT * FROM v_messages WHERE user_email = ? AND id IN (%s)", placeholders)
	
	args := make([]interface{}, len(missing)+1)
	args[0] = email
	for i, id := range missing {
		args[i+1] = int(id)
	}

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil { return nil, err }
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil { return nil, err }
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func GetIncompleteByThreadID(ctx context.Context, q Querier, email, threadID string) ([]ConsolidatedMessage, error) {
	if threadID == "" {
		return []ConsolidatedMessage{}, nil
	}
	rows, err := q.QueryContext(ctx, SQL.GetIncompleteByThreadID, email, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetActiveContextTasks retrieves a subset of incomplete tasks to provide context for AI analysis.
// Why: Limits results to 50 items and 30 days to optimize AI token usage and memory overhead.
func GetActiveContextTasks(ctx context.Context, q Querier, email, source, room string) ([]ConsolidatedMessage, error) {
	rows, err := q.QueryContext(ctx, SQL.GetActiveTasksForContext, email, source, room)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanContextTaskRow(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func scanContextTaskRow(rows *sql.Rows) (ConsolidatedMessage, error) {
	var m ConsolidatedMessage
	err := rows.Scan(&m.ID, &m.Task, &m.OriginalText, &m.Requester, &m.Assignee, &m.Source, &m.Room, &m.AssignedAt, &m.Done, &m.CompletedAt, &m.Category)
	return m, err
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
