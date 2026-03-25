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
	err := db.QueryRow(SQL.SaveMessage, msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline, msg.ThreadID).Scan(&lastID)

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
	valueArgs := make([]interface{}, 0, len(toInsert)*13)

	for _, msg := range toInsert {
		valueStrings = append(valueStrings, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		valueArgs = append(valueArgs, msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText, msg.Category, msg.Deadline, msg.ThreadID)
	}

	query := fmt.Sprintf(`INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id) 
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
	var completedAt interface{}
	now := time.Now()
	if done {
		completedAt = now
	} else {
		completedAt = nil
	}

	_, err := db.Exec(SQL.MarkMessageDone, done, completedAt, id, email)
	if err == nil {
		// 즉시 로컬 캐시 업데이트 (UI 반응성 개선)
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

		// 백그라운드에서 전체 캐시 정합성 확보
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
	query := fmt.Sprintf(SQL.DeleteMessages, strings.Repeat("?,", len(ids)-1)+"?")
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = id
	}
	_, err := db.Exec(query, args...)
	if err == nil {
		// 즉시 로컬 캐시에서 제거
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

func HardDeleteMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	query := fmt.Sprintf(SQL.HardDeleteMessages, strings.Repeat("?,", len(ids)-1)+"?")
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = id
	}
	_, err := db.Exec(query, args...)
	if err == nil {
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

		var newArchive []ConsolidatedMessage
		for _, m := range archiveCache[email] {
			if !idMap[m.ID] {
				newArchive = append(newArchive, m)
			}
		}
		archiveCache[email] = newArchive
		cacheMu.Unlock()

		go func() { _ = RefreshCache(email) }()
	}
	return err
}

func RestoreMessages(email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	query := fmt.Sprintf(SQL.RestoreMessages, strings.Repeat("?,", len(ids)-1)+"?")
	args := make([]interface{}, len(ids)+1)
	args[0] = email
	for i, id := range ids {
		args[i+1] = id
	}
	_, err := db.Exec(query, args...)
	if err == nil {
		// Restore is complex since it might change categories back to active/archive.
		// For simplicity, just trigger background refresh immediately.
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
	query := fmt.Sprintf(SQL.GetMessagesByIDs, strings.Repeat("?,", len(ids)-1)+"?")
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
