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
func SaveMessage(msg ConsolidatedMessage) (bool, int, error) {
	if isDuplicate(msg.UserEmail, msg.SourceTS) {
		return false, 0, nil
	}

	msg.Requester = NormalizeName(msg.UserEmail, msg.Requester)
	msg.Assignee = NormalizeName(msg.UserEmail, msg.Assignee)

	lastID, err := insertMessage(msg)
	if err != nil || lastID == 0 {
		return false, lastID, err
	}

	// Why: Notifies the caching layer to reload this user's data on the next read.
	InvalidateCache(msg.UserEmail)
	return true, lastID, nil
}

func isDuplicate(email, ts string) bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	userKnown, ok := knownTS[email]
	return ok && userKnown[ts]
}

// SaveMessages performs a bulk insert of multiple messages.
// Why: Refactored to satisfy 30-line limit by delegating bulk preparation, DB execution, and multi-user cache updates.
func SaveMessages(msgs []ConsolidatedMessage) ([]int, error) {
	toInsert := filterNewOnly(msgs)
	if len(toInsert) == 0 {
		return nil, nil
	}

	normalizeMsgs(toInsert)
	newIDsMap, err := executeBulkInsert(toInsert)
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

func executeBulkInsert(msgs []ConsolidatedMessage) (map[string]map[string]int, error) {
	// Why: [WhaTap-Memory] Bulk insert builds a large argument slice; 17 fields per message.
	valueStrings := make([]string, 0, len(msgs))
	valueArgs := make([]interface{}, 0, len(msgs)*18)

	for _, msg := range msgs {
		valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		constraintsJSON, _ := json.Marshal(msg.Constraints)
		valueArgs = append(valueArgs, 
			msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, 
			msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText, msg.Category, 
			msg.Deadline, msg.ThreadID, msg.AssigneeReason, msg.RepliedToID, 
			msg.IsContextQuery, string(constraintsJSON), string(msg.Metadata),
		)
	}

	query := fmt.Sprintf(SQL.SaveMessagesBase, strings.Join(valueStrings, ","))
	rows, err := db.Query(query, valueArgs...)
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

func insertMessage(msg ConsolidatedMessage) (int, error) {
	var lastID int
	constraintsJSON, _ := json.Marshal(msg.Constraints)
	err := db.QueryRow(SQL.SaveMessage,
		msg.UserEmail, msg.Source, msg.Room, msg.Task,
		msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link,
		msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline,
		msg.ThreadID, msg.AssigneeReason, msg.RepliedToID,
		msg.IsContextQuery, string(constraintsJSON), string(msg.Metadata),
	).Scan(&lastID)
	if err != nil && err != sql.ErrNoRows {
		logger.Errorf("SaveMessage DB Scan Error: %v", err)
		return 0, err
	}
	return lastID, nil
}

func GetMessages(email string) ([]ConsolidatedMessage, error) {
	if err := EnsureCacheInitialized(email); err != nil {
		return nil, err
	}

	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if msgs, ok := messageCache[email]; ok {
		return msgs, nil
	}
	return []ConsolidatedMessage{}, nil
}

func MarkMessageDone(email string, id int, done bool) error {
	var comp interface{} = nil
	if done { comp = time.Now() }

	if _, err := db.Exec(SQL.MarkMessageDone, done, comp, int(id), email); err != nil {
		return err
	}
	// Why: Immediate invalidation ensures periodic UI polling fetches the strictly consistent state.
	InvalidateCache(email)
	return nil
}

func UpdateTaskText(email string, id int, task string) error {
	if _, err := db.Exec(SQL.UpdateTaskText, task, int(id), email); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

// UpdateTaskDescriptionAppend appends new content to the task text only.
// Why: Called when consolidating tasks from the same source message to prevent original_text duplication.
func UpdateTaskDescriptionAppend(id int, date, newTask string) error {
	_, err := db.Exec(SQL.UpdateTaskDescriptionAppend, date, newTask, int(id))
	return err
}

// UpdateTaskFullAppend appends new content to both task and original_text.
// Why: Called when consolidating tasks from different source messages where full context must be preserved.
func UpdateTaskFullAppend(id int, date, newTask, newOriginalText string) error {
	_, err := db.Exec(SQL.UpdateTaskFullAppend, date, newTask, newOriginalText, int(id))
	return err
}

func UpdateMessageCategory(email string, id int, category string) error {
	if _, err := db.Exec(SQL.UpdateMessageCategory, category, int(id), email); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func UpdateTaskAssignee(email string, id int, assignee string) error {
	if _, err := db.Exec(SQL.UpdateTaskAssignee, assignee, int(id), email); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func DeleteMessages(email string, ids []int) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (%s)", placeholders)
	args := prepareIDArgs(email, ids)
	if _, err := db.Exec(query, args...); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func HardDeleteMessages(email string, ids []int) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("DELETE FROM messages WHERE user_email = ? AND id IN (%s)", placeholders)
	args := prepareIDArgs(email, ids)
	if _, err := db.Exec(query, args...); err != nil {
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

func RestoreMessages(email string, ids []int) error {
	if len(ids) == 0 { return nil }
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("UPDATE messages SET is_deleted = 0, done = 0, completed_at = NULL WHERE user_email = ? AND id IN (%s)", placeholders)
	args := prepareIDArgs(email, ids)
	if _, err := db.Exec(query, args...); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func GetMessageByID(ctx context.Context, id int) (ConsolidatedMessage, error) {
	row := db.QueryRowContext(ctx, SQL.GetMessageByID, int(id))
	return scanMessageRow(row)
}

func GetMessagesByIDs(ctx context.Context, ids []int) ([]ConsolidatedMessage, error) {
	if len(ids) == 0 {
		return []ConsolidatedMessage{}, nil
	}

	//Why: Explicitly specifies all 20 columns from the v_messages view to ensure identity-resolved fields are correctly scanned into the struct.
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf(SQL.GetMessagesByIDs, placeholders)
	interfaceIds := make([]interface{}, len(ids))
	for i, v := range ids {
		interfaceIds[i] = int(v)
	}
	rows, err := db.QueryContext(ctx, query, interfaceIds...)
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

func GetIncompleteByThreadID(ctx context.Context, email, threadID string) ([]ConsolidatedMessage, error) {
	if threadID == "" {
		return []ConsolidatedMessage{}, nil
	}
	rows, err := db.QueryContext(ctx, SQL.GetIncompleteByThreadID, email, threadID)
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
func GetActiveContextTasks(ctx context.Context, email, source, room string) ([]ConsolidatedMessage, error) {
	rows, err := db.QueryContext(ctx, SQL.GetActiveTasksForContext, email, source, room)
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
	err := rows.Scan(&m.ID, &m.Task, &m.OriginalText, &m.Requester, &m.Assignee, &m.Source, &m.Room, &m.AssignedAt, &m.Done, &m.CompletedAt)
	return m, err
}

// CategorizeByUser groups a slice of messages into dashboard categories.
// Why: [SSOT] Unifies backend and frontend filtering logic into a single Go implementation.
func CategorizeByUser(msgs []ConsolidatedMessage, userName string, aliases []string) CategorizedMessages {
	res := CategorizedMessages{
		Inbox:   make([]ConsolidatedMessage, 0),
		Pending: make([]ConsolidatedMessage, 0),
		Waiting: make([]ConsolidatedMessage, 0),
		All:     msgs,
	}
	for _, m := range msgs {
		if m.Category == "waiting" {
			res.Waiting = append(res.Waiting, m)
		} else if IsAssignedToUser(m.Assignee, userName, aliases) {
			res.Inbox = append(res.Inbox, m)
		} else {
			res.Pending = append(res.Pending, m)
		}
	}
	return res
}

func IsAssignedToUser(assignee, name string, aliases []string) bool {
	if assignee == "me" || strings.EqualFold(assignee, name) {
		return true
	}
	for _, a := range aliases {
		if strings.EqualFold(assignee, a) {
			return true
		}
	}
	return false
}
