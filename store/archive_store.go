package store

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func GetArchivedMessages(email string) ([]ConsolidatedMessage, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if msgs, ok := archiveCache[email]; ok {
		return msgs, nil
	}
	return []ConsolidatedMessage{}, nil
}

func getArchiveDays() int {
	if envDays := os.Getenv("ARCHIVE_DAYS"); envDays != "" {
		if parsed, err := strconv.Atoi(envDays); err == nil && parsed >= 0 {
			return parsed
		}
	}
	if autoArchiveDays > 0 {
		return autoArchiveDays
	}
	return 1 // 기본값 1일
}

func GetArchivedMessagesFiltered(ctx context.Context, filter ArchiveFilter) ([]ConsolidatedMessage, int, error) {
	searchQuery := ""
	args := []interface{}{filter.Email}

	if filter.Query != "" {
		pattern := "%" + strings.ToLower(filter.Query) + "%"
		searchQuery = ` AND (
			LOWER(task) LIKE ? OR 
			LOWER(room) LIKE ? OR 
			LOWER(requester) LIKE ? OR 
			LOWER(original_text) LIKE ? OR
			LOWER(source) LIKE ? OR
			LOWER(assignee) LIKE ?
		)`
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern)
	}

	// 1. Get Count
	safeArchiveDays := getArchiveDays()

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM messages 
		WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', '-%d days')))
		%s`, safeArchiveDays, searchQuery)

	var total int
	err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// 2. Get Data
	if filter.Limit <= 0 {
		filter.Limit = 100
	}

	orderBy := "CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC"
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

	if dbField, ok := whitelist[filter.Sort]; ok {
		order := "ASC"
		if strings.ToUpper(filter.Order) == "DESC" {
			order = "DESC"
		}
		orderBy = fmt.Sprintf("%s %s", dbField, order)
	}

	dataQuery := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at, COALESCE(category, 'todo'), COALESCE(deadline, '') 
		FROM messages 
		WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', '-%d days')))
		%s
		ORDER BY %s
		LIMIT ? OFFSET ?`, safeArchiveDays, searchQuery, orderBy)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		var m ConsolidatedMessage
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt, &m.Category, &m.Deadline); err != nil {
			return nil, 0, err
		}
		msgs = append(msgs, m)
	}

	return msgs, total, nil
}

// GetArchivedMessagesCount fetches strictly the total count of archived items, skipping the data query.
func GetArchivedMessagesCount(ctx context.Context, filter ArchiveFilter) (int, error) {
	searchQuery := ""
	args := []interface{}{filter.Email}

	if filter.Query != "" {
		pattern := "%" + strings.ToLower(filter.Query) + "%"
		searchQuery = ` AND (
			LOWER(task) LIKE ? OR 
			LOWER(room) LIKE ? OR 
			LOWER(requester) LIKE ? OR 
			LOWER(original_text) LIKE ? OR
			LOWER(source) LIKE ? OR
			LOWER(assignee) LIKE ?
		)`
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern)
	}

	safeArchiveDays := getArchiveDays()

	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM messages 
		WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', '-%d days')))
		%s`, safeArchiveDays, searchQuery)

	var total int
	err := db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	return total, err
}

