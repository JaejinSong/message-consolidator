package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/logger"
	"strings"
	"time"
)

func SaveMessage(msg ConsolidatedMessage) (bool, int, error) {
	cacheMu.RLock()
	if userKnown, ok := knownTS[msg.UserEmail]; ok && userKnown[msg.SourceTS] {
		cacheMu.RUnlock()
		return false, 0, nil
	}
	cacheMu.RUnlock()

	msg.Requester = NormalizeName(msg.UserEmail, msg.Requester)
	msg.Assignee = NormalizeName(msg.UserEmail, msg.Assignee)

	var lastID int
	query := `INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			  ON CONFLICT(user_email, source_ts) DO NOTHING
			  RETURNING id;`
	err := db.QueryRow(query, msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline).Scan(&lastID)

	if err != nil {
		if err == sql.ErrNoRows {
			return false, 0, nil
		}
		logger.Errorf("SaveMessage Error: %v", err)
		return false, 0, err
	}

	msg.ID = lastID
	msg.CreatedAt = time.Now()

	cacheMu.Lock()
	if _, ok := knownTS[msg.UserEmail]; !ok {
		knownTS[msg.UserEmail] = make(map[string]bool)
	}
	knownTS[msg.UserEmail][msg.SourceTS] = true
	messageCache[msg.UserEmail] = append([]ConsolidatedMessage{msg}, messageCache[msg.UserEmail]...)
	cacheMu.Unlock()

	return true, lastID, nil
}

func SaveMessages(msgs []ConsolidatedMessage) ([]int, error) {
	if len(msgs) == 0 {
		return nil, nil
	}

	var toInsert []ConsolidatedMessage
	cacheMu.RLock()
	for _, msg := range msgs {
		if userKnown, ok := knownTS[msg.UserEmail]; ok && userKnown[msg.SourceTS] {
			continue
		}
		toInsert = append(toInsert, msg)
	}
	cacheMu.RUnlock()

	if len(toInsert) == 0 {
		return nil, nil
	}

	for i := range toInsert {
		toInsert[i].Requester = NormalizeName(toInsert[i].UserEmail, toInsert[i].Requester)
		toInsert[i].Assignee = NormalizeName(toInsert[i].UserEmail, toInsert[i].Assignee)
	}

	valueStrings := make([]string, 0, len(toInsert))
	valueArgs := make([]interface{}, 0, len(toInsert)*11)

	for i, msg := range toInsert {
		offset := i * 12
		valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6, offset+7, offset+8, offset+9, offset+10, offset+11, offset+12))
		valueArgs = append(valueArgs, msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline)
	}

	query := fmt.Sprintf(`INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline) 
			  VALUES %s
			  ON CONFLICT(user_email, source_ts) DO NOTHING
			  RETURNING id, source_ts, user_email;`, strings.Join(valueStrings, ","))

	rows, err := db.Query(query, valueArgs...)
	if err != nil {
		logger.Errorf("SaveMessages Bulk Insert Error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var newIDs []int
	now := time.Now()
	insertedIDs := make(map[string]map[string]int)

	for rows.Next() {
		var id int
		var ts, email string
		if err := rows.Scan(&id, &ts, &email); err == nil {
			newIDs = append(newIDs, id)
			if insertedIDs[email] == nil {
				insertedIDs[email] = make(map[string]int)
			}
			insertedIDs[email][ts] = id
		}
	}

	cacheMu.Lock()
	for _, msg := range toInsert {
		if id, ok := insertedIDs[msg.UserEmail][msg.SourceTS]; ok {
			msg.ID = id
			msg.CreatedAt = now
			if _, exists := knownTS[msg.UserEmail]; !exists {
				knownTS[msg.UserEmail] = make(map[string]bool)
			}
			knownTS[msg.UserEmail][msg.SourceTS] = true
			messageCache[msg.UserEmail] = append([]ConsolidatedMessage{msg}, messageCache[msg.UserEmail]...)
		}
	}
	cacheMu.Unlock()

	return newIDs, nil
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
	var completeTime *time.Time = nil
	if done {
		now := time.Now()
		completeTime = &now
	}
	res, err := db.Exec("UPDATE messages SET done = $1, completed_at = $2 WHERE id = $3 AND user_email = $4", done, completeTime, id, email)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	logger.Debugf("[STORE] Mark message ID %d done=%v, affected rows: %d", id, done, rows)

	if rows > 0 {
		_ = RefreshCache(email)
	}

	return nil
}

func UpdateTaskText(email string, id int, task string) error {
	res, err := db.Exec("UPDATE messages SET task = $1 WHERE id = $2 AND user_email = $3", task, id, email)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	if rows > 0 {
		_ = RefreshCache(email)
	}

	return nil
}

func UpdateTaskAssignee(email string, id int, assignee string) error {
	res, err := db.Exec("UPDATE messages SET assignee = $1 WHERE id = $2 AND user_email = $3", assignee, id, email)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()
	if rows > 0 {
		_ = RefreshCache(email)
	}

	return nil
}

func DeleteMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf("UPDATE messages SET is_deleted = true WHERE user_email = $1 AND id IN (%s)", strings.Join(placeholders, ","))
	res, err := db.Exec(query, args...)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()

	if rows > 0 {
		_ = RefreshCache(email)
	}

	return nil
}

func HardDeleteMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf("DELETE FROM messages WHERE user_email = $1 AND id IN (%s)", strings.Join(placeholders, ","))
	res, err := db.Exec(query, args...)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()

	if rows > 0 {
		_ = RefreshCache(email)
	}

	return nil
}

func RestoreMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf("UPDATE messages SET is_deleted = false, done = false, completed_at = NULL WHERE user_email = $1 AND id IN (%s)", strings.Join(placeholders, ","))
	res, err := db.Exec(query, args...)
	if err != nil {
		return err
	}

	rows, _ := res.RowsAffected()

	if rows > 0 {
		// 메모리 캐시 정합성을 위해 해당 사용자의 캐시를 전체 갱신합니다.
		if err := RefreshCache(email); err != nil {
			logger.Errorf("[STORE] RefreshCache error during restore for %s: %v", email, err)
			return err
		}
	}

	return nil
}

func GetMessageByID(ctx context.Context, id int) (ConsolidatedMessage, error) {
	var m ConsolidatedMessage
	err := db.QueryRowContext(ctx, "SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at, COALESCE(category, 'todo'), COALESCE(deadline, '') FROM messages WHERE id = $1", id).Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt, &m.Category, &m.Deadline)
	if err != nil {
		return m, err
	}
	return m, nil
}

func GetMessagesByIDs(ctx context.Context, ids []int) ([]ConsolidatedMessage, error) {
	if len(ids) == 0 {
		return []ConsolidatedMessage{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at, COALESCE(category, 'todo'), COALESCE(deadline, '') 
	          FROM messages 
	          WHERE id IN (%s)`, strings.Join(placeholders, ","))
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		var m ConsolidatedMessage
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt, &m.Category, &m.Deadline); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}
