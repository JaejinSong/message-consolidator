package store

import (
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
	query := `INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			  ON CONFLICT(user_email, source_ts) DO NOTHING
			  RETURNING id;`
	err := db.QueryRow(query, msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText).Scan(&lastID)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return false, 0, nil
		}
		logger.Errorf("SaveMessage Error: %v", err)
		return false, 0, err
	}

	cacheMu.Lock()
	if _, ok := knownTS[msg.UserEmail]; !ok {
		knownTS[msg.UserEmail] = make(map[string]bool)
	}
	knownTS[msg.UserEmail][msg.SourceTS] = true
	cacheMu.Unlock()

	return true, lastID, nil
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
	_, err := db.Exec("UPDATE messages SET done = $1, completed_at = $2 WHERE id = $3 AND user_email = $4", done, completeTime, id, email)
	if err != nil {
		return err
	}

	// Local Cache Patching
	cacheMu.Lock()
	defer cacheMu.Unlock()

	patch := func(list []ConsolidatedMessage) {
		for i := range list {
			if list[i].ID == id {
				list[i].Done = done
				list[i].CompletedAt = completeTime
				break
			}
		}
	}
	patch(messageCache[email])
	patch(archiveCache[email])

	logger.Debugf("[STORE] Patched DONE state in messageCache for user %s, msgID %d (Done=%v)", email, id, done)

	return nil
}

func GetArchivedMessages(email string) ([]ConsolidatedMessage, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if msgs, ok := archiveCache[email]; ok {
		return msgs, nil
	}
	return []ConsolidatedMessage{}, nil
}

func GetArchivedMessagesFiltered(email string, limit, offset int, search string, sortField, sortOrder string) ([]ConsolidatedMessage, int, error) {
	searchQuery := ""
	args := []interface{}{email}
	argIdx := 2

	if search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		searchQuery = fmt.Sprintf(` AND (
			LOWER(task) ILIKE $%d OR 
			LOWER(room) ILIKE $%d OR 
			LOWER(requester) ILIKE $%d OR 
			LOWER(original_text) ILIKE $%d OR
			LOWER(source) ILIKE $%d
		)`, argIdx, argIdx, argIdx, argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}

	// 1. Get Count
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = true OR (done = true AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '6 days'))
		%s`, searchQuery)
	
	var total int
	err := db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// 2. Get Data
	if limit <= 0 {
		limit = 100
	}
	
	orderBy := "CASE WHEN is_deleted = true THEN created_at ELSE completed_at END DESC"
	whitelist := map[string]string{
		"source":       "source",
		"room":         "room",
		"task":         "task",
		"requester":    "requester",
		"assignee":     "assignee",
		"created_at":   "created_at",
		"completed_at": "completed_at",
		"time":         "created_at",
	}

	if sortField != "" {
		if dbField, ok := whitelist[sortField]; ok {
			order := "ASC"
			if strings.ToUpper(sortOrder) == "DESC" {
				order = "DESC"
			}
			orderBy = fmt.Sprintf("%s %s", dbField, order)
		}
	}

	dataQuery := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = true OR (done = true AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '6 days'))
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, searchQuery, orderBy, argIdx, argIdx+1)
	
	args = append(args, limit, offset)
	
	rows, err := db.Query(dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		var m ConsolidatedMessage
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt); err != nil {
			return nil, 0, err
		}
		msgs = append(msgs, m)
	}

	return msgs, total, nil
}

func UpdateTaskText(email string, id int, task string) error {
	_, err := db.Exec("UPDATE messages SET task = $1 WHERE id = $2 AND user_email = $3", task, id, email)
	if err != nil {
		return err
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()

	patch := func(list []ConsolidatedMessage) {
		for i := range list {
			if list[i].ID == id {
				list[i].Task = task
				break
			}
		}
	}
	patch(messageCache[email])
	patch(archiveCache[email])

	return nil
}

func DeleteMessage(email string, id int) error {
	res, err := db.Exec("UPDATE messages SET is_deleted = true WHERE id = $1 AND user_email = $2", id, email)
	if err != nil {
		return err
	}
	
	rows, _ := res.RowsAffected()
	logger.Debugf("[DB] Soft-delete message ID %d, affected rows: %d", id, rows)
	
	cacheMu.Lock()
	defer cacheMu.Unlock()
	
	patch := func(list []ConsolidatedMessage) {
		for i := range list {
			if list[i].ID == id {
				list[i].IsDeleted = true
				break
			}
		}
	}
	patch(messageCache[email])
	patch(archiveCache[email])

	return nil
}

func HardDeleteMessage(email string, id int) error {
	res, err := db.Exec("DELETE FROM messages WHERE id = $1 AND user_email = $2", id, email)
	if err != nil {
		return err
	}
	
	rows, _ := res.RowsAffected()
	logger.Debugf("[DB] Hard-delete message ID %d, affected rows: %d", id, rows)
	
	cacheMu.Lock()
	defer cacheMu.Unlock()
	
	removeFromList := func(list []ConsolidatedMessage) []ConsolidatedMessage {
		for i := range list {
			if list[i].ID == id {
				return append(list[:i], list[i+1:]...)
			}
		}
		return list
	}
	messageCache[email] = removeFromList(messageCache[email])
	archiveCache[email] = removeFromList(archiveCache[email])

	return nil
}

func RestoreMessage(email string, id int) error {
	res, err := db.Exec("UPDATE messages SET is_deleted = false WHERE id = $1 AND user_email = $2", id, email)
	if err != nil {
		return err
	}
	
	rows, _ := res.RowsAffected()
	logger.Debugf("[DB] Restore message ID %d, affected rows: %d", id, rows)
	
	cacheMu.Lock()
	defer cacheMu.Unlock()
	
	patch := func(list []ConsolidatedMessage) {
		for i := range list {
			if list[i].ID == id {
				list[i].IsDeleted = false
				break
			}
		}
	}
	patch(messageCache[email])
	patch(archiveCache[email])

	return nil
}

func GetMessageByID(id int) (ConsolidatedMessage, error) {
	var m ConsolidatedMessage
	err := db.QueryRow("SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at FROM messages WHERE id = $1", id).Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt)
	if err != nil {
		return m, err
	}
	return m, nil
}
