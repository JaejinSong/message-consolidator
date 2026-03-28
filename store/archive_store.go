package store

import (
	"context"
	"fmt"
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

var autoArchiveDays int = 7

func SetAutoArchiveDays(days int) {
	if days > 0 {
		autoArchiveDays = days
	}
}

func GetAutoArchiveDays() int {
	return autoArchiveDays
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

	// Why: Prioritizes completion status over deletion status as per user preference.
	// Done tab shows all completed items (regardless of deletion).
	// Canceled tab shows uncompleted but deleted items (abandoned tasks).
	switch filter.Status {
	case "done":
		searchQuery += " AND (done = 1 OR done = TRUE)"
	case "canceled":
		searchQuery += " AND (done = 0 OR done = FALSE) AND (is_deleted = 1 OR is_deleted = TRUE)"
	}

	// Why: We need the total count of filtered items separately to support pagination on the frontend.
	safeArchiveDays := GetAutoArchiveDays()
	daysParam := fmt.Sprintf("-%d days", safeArchiveDays)
	countQuery := SQL.GetArchivedMessagesCountBase + searchQuery

	var total int
	err := db.QueryRowContext(ctx, countQuery, append([]interface{}{filter.Email, daysParam}, args[1:]...)...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Why: Prevent unbounded queries that could crash the server or overload the database if the client doesn't specify a limit.
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

	dataQuery := fmt.Sprintf("%s %s ORDER BY %s LIMIT ? OFFSET ?", SQL.GetArchivedMessagesBase, searchQuery, orderBy)
	finalArgs := append([]interface{}{filter.Email, daysParam}, args[1:]...)
	finalArgs = append(finalArgs, filter.Limit, filter.Offset)

	rows, err := db.QueryContext(ctx, dataQuery, finalArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
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

	// Why: Ensures the pagination count matches the new 'Completion Priority' logic.
	switch filter.Status {
	case "done":
		searchQuery += " AND (done = 1 OR done = TRUE)"
	case "canceled":
		searchQuery += " AND (done = 0 OR done = FALSE) AND (is_deleted = 1 OR is_deleted = TRUE)"
	}

	safeArchiveDays := GetAutoArchiveDays()
	daysParam := fmt.Sprintf("-%d days", safeArchiveDays)
	countQuery := SQL.GetArchivedMessagesCountBase + searchQuery

	var total int
	err := db.QueryRowContext(ctx, countQuery, append([]interface{}{filter.Email, daysParam}, args[1:]...)...).Scan(&total)
	return total, err
}
