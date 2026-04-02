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
	if err != nil {
		return false, 0, err
	}
	if lastID == 0 {
		return false, 0, nil
	}

	msg.ID = lastID
	msg.CreatedAt = time.Now()
	updateCache(msg)

	return true, lastID, nil
}

func isDuplicate(email, ts string) bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	userKnown, ok := knownTS[email]
	return ok && userKnown[ts]
}

func insertMessage(msg ConsolidatedMessage) (int, error) {
	var lastID int
	// Why: [WhaTap-Memory] Constraints array is serialized to JSON string before DB entry to keep memory footprint predictable during persistence.
	constraintsJSON, _ := json.Marshal(msg.Constraints)
	err := db.QueryRow(SQL.SaveMessage,
		msg.UserEmail, msg.Source, msg.Room, msg.Task,
		msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link,
		msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline,
		msg.ThreadID, msg.AssigneeReason, msg.RepliedToID,
		msg.IsContextQuery, string(constraintsJSON), string(msg.Metadata),
	).Scan(&lastID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // Row was not inserted due to conflict (duplicate TS)
		}
		logger.Errorf("SaveMessage DB Scan Error: %v (msgID: %d)", err, lastID)
		return 0, err
	}
	return lastID, nil
}

func updateCache(msg ConsolidatedMessage) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if _, ok := knownTS[msg.UserEmail]; !ok {
		knownTS[msg.UserEmail] = make(map[string]bool)
	}
	knownTS[msg.UserEmail][msg.SourceTS] = true
	messageCache[msg.UserEmail] = append([]ConsolidatedMessage{msg}, messageCache[msg.UserEmail]...)
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

	updateBulkCache(toInsert, newIDsMap)
	
	ids := make([]int, 0, len(newIDsMap))
	for _, userMap := range newIDsMap {
		for _, id := range userMap {
			ids = append(ids, id)
		}
	}
	return ids, nil
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

func updateBulkCache(msgs []ConsolidatedMessage, idMap map[string]map[string]int) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	now := time.Now()
	for _, msg := range msgs {
		if id, ok := idMap[msg.UserEmail][msg.SourceTS]; ok {
			msg.ID = id
			msg.CreatedAt = now
			if _, exists := knownTS[msg.UserEmail]; !exists {
				knownTS[msg.UserEmail] = make(map[string]bool)
			}
			knownTS[msg.UserEmail][msg.SourceTS] = true
			messageCache[msg.UserEmail] = append([]ConsolidatedMessage{msg}, messageCache[msg.UserEmail]...)
		}
	}
}

func GetMessages(email string) ([]ConsolidatedMessage, error) {
	if err := EnsureCacheInitialized(email); err != nil {
		logger.Errorf("Failed to ensure cache initialized for %s in GetMessages: %v", email, err)
	}

	cacheMu.RLock()
	msgs := messageCache[email]
	cacheMu.RUnlock()

	if msgs == nil {
		return []ConsolidatedMessage{}, nil
	}
	return msgs, nil
}

func MarkMessageDone(email string, id int, done bool) error {
	var completedAt interface{}
	now := time.Now()
	if done {
		completedAt = now
	} else {
		completedAt = nil
	}

	_, err := db.Exec(SQL.MarkMessageDone, done, completedAt, id, email)
	if err == nil {
		//Why: Immediately updates the local cache to improve UI responsiveness before the background refresh completes.
		cacheMu.Lock()
		for i := range messageCache[email] {
			if messageCache[email][i].ID == id {
				messageCache[email][i].Done = done
				if done {
					messageCache[email][i].CompletedAt = &now
				} else {
					messageCache[email][i].CompletedAt = nil
				}
				break
			}
		}
		cacheMu.Unlock()

		//Why: Triggers a background cache refresh to ensure long-term data consistency across the application.
		go func() {
			if err := RefreshCache(email); err != nil {
				logger.Errorf("Background RefreshCache error for %s: %v", email, err)
			}
		}()
	}
	return err
}

func UpdateTaskText(email string, id int, task string) error {
	_, err := db.Exec(SQL.UpdateTaskText, task, id, email)
	if err == nil {
		cacheMu.Lock()
		for i := range messageCache[email] {
			if messageCache[email][i].ID == id {
				messageCache[email][i].Task = task
				break
			}
		}
		cacheMu.Unlock()

		go func() { _ = RefreshCache(email) }()
	}
	return err
}

func UpdateMessageCategory(email string, id int, category string) error {
	_, err := db.Exec(SQL.UpdateMessageCategory, category, id, email)
	if err == nil {
		cacheMu.Lock()
		for i := range messageCache[email] {
			if messageCache[email][i].ID == id {
				messageCache[email][i].Category = category
				break
			}
		}
		cacheMu.Unlock()

		go func() { _ = RefreshCache(email) }()
	}
	return err
}

func UpdateTaskAssignee(email string, id int, assignee string) error {
	_, err := db.Exec(SQL.UpdateTaskAssignee, assignee, id, email)
	if err == nil {
		cacheMu.Lock()
		for i := range messageCache[email] {
			if messageCache[email][i].ID == id {
				messageCache[email][i].Assignee = assignee
				break
			}
		}
		cacheMu.Unlock()

		go func() { _ = RefreshCache(email) }()
	}
	return err
}

func DeleteMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (%s)", placeholders)
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = id
	}
	_, err := db.Exec(query, args...)
	if err == nil {
		//Why: Immediately removes deleted messages from the local cache for instant UI feedback.
		cacheMu.Lock()
		idMap := make(map[int]bool)
		for _, id := range ids {
			idMap[id] = true
		}
		var newActive []ConsolidatedMessage
		for _, m := range messageCache[email] {
			if !idMap[m.ID] {
				newActive = append(newActive, m)
			}
		}
		messageCache[email] = newActive
		cacheMu.Unlock()

		go func() { _ = RefreshCache(email) }()
	}
	return err
}

// HardDeleteMessages removes messages permanently from both active and archive caches.
// Why: Delegates cache filtering to a reusable helper to comply with the 30-line limit.
func HardDeleteMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("DELETE FROM messages WHERE user_email = ? AND id IN (%s)", placeholders)
	
	args := prepareIDArgs(email, ids)
	if _, err := db.Exec(query, args...); err != nil {
		return err
	}

	updateHardDeleteCache(email, ids)
	return nil
}

func prepareIDArgs(email string, ids []int) []interface{} {
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = id
	}
	return args
}

func updateHardDeleteCache(email string, ids []int) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	idMap := make(map[int]bool)
	for _, id := range ids {
		idMap[id] = true
	}
	messageCache[email] = filterCache(messageCache[email], idMap)
	archiveCache[email] = filterCache(archiveCache[email], idMap)
	
	go func() { _ = RefreshCache(email) }()
}

func filterCache(cache []ConsolidatedMessage, idMap map[int]bool) []ConsolidatedMessage {
	var filtered []ConsolidatedMessage
	for _, m := range cache {
		if !idMap[m.ID] {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func RestoreMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf("UPDATE messages SET is_deleted = 0 WHERE user_email = ? AND id IN (%s)", placeholders)
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = id
	}
	_, err := db.Exec(query, args...)
	if err == nil {
		//Why: Triggers an immediate background refresh because restoration logic can complexly affect multiple categories.
		go func() { _ = RefreshCache(email) }()
	}
	return err
}

func GetMessageByID(ctx context.Context, id int) (ConsolidatedMessage, error) {
	row := db.QueryRowContext(ctx, SQL.GetMessageByID, id)
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
		interfaceIds[i] = v
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
