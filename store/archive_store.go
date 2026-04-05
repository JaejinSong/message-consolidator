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
	// [Guard Clause] Serve from cache for first-page, non-search requests to reduce DB load.
	if filter.Query == "" && filter.Offset == 0 && filter.Limit >= 50 {
		if msgs, total, ok := getFromArchiveCache(filter); ok {
			return msgs, total, nil
		}
	}
	return fetchArchivedFromDB(ctx, filter)
}

func getFromArchiveCache(f ArchiveFilter) ([]ConsolidatedMessage, int, bool) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	
	raw, ok := archiveCache[f.Email]
	if !ok { return nil, 0, false }

	filtered := filterByStatus(raw, f.Status)
	// Why: Only return cache if it likely contains the full set of recent items (count < cache limit).
	if len(raw) >= 100 && len(filtered) < f.Limit {
		return nil, 0, false
	}

	limit := f.Limit
	if len(filtered) < limit { limit = len(filtered) }
	
	return filtered[:limit], len(filtered), true
}

func filterByStatus(msgs []ConsolidatedMessage, status string) []ConsolidatedMessage {
	var filtered []ConsolidatedMessage
	for _, m := range msgs {
		if statusMatch(m, status) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

func statusMatch(m ConsolidatedMessage, status string) bool {
	switch status {
	case "done":
		return m.Done
	case "canceled":
		return !m.Done && m.IsDeleted
	case "merged":
		return m.Category == "merged"
	default:
		return true
	}
}

func fetchArchivedFromDB(ctx context.Context, filter ArchiveFilter) ([]ConsolidatedMessage, int, error) {
	searchQuery, args := buildArchiveQuery(filter)
	
	safeArchiveDays := GetAutoArchiveDays()
	daysParam := fmt.Sprintf("-%d days", safeArchiveDays)
	
	var total int
	countQuery := SQL.GetArchivedMessagesCountBase + searchQuery
	err := db.QueryRowContext(ctx, countQuery, append([]interface{}{filter.Email, daysParam}, args[1:]...)...).Scan(&total)
	if err != nil { return nil, 0, err }

	if filter.Limit <= 0 { filter.Limit = 100 }
	
	orderBy := "CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC"
	dataQuery := fmt.Sprintf("%s %s ORDER BY %s LIMIT ? OFFSET ?", SQL.GetArchivedMessagesBase, searchQuery, orderBy)
	finalArgs := append([]interface{}{filter.Email, daysParam}, args[1:]...)
	finalArgs = append(finalArgs, filter.Limit, filter.Offset)

	rows, err := db.QueryContext(ctx, dataQuery, finalArgs...)
	if err != nil { return nil, 0, err }
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil { return nil, 0, err }
		msgs = append(msgs, m)
	}
	return msgs, total, rows.Err()
}

func buildArchiveQuery(f ArchiveFilter) (string, []interface{}) {
	query := ""
	args := []interface{}{f.Email}
	if f.Query != "" {
		pattern := "%" + strings.ToLower(f.Query) + "%"
		query = ` AND (LOWER(task) LIKE ? OR LOWER(room) LIKE ? OR LOWER(requester) LIKE ? OR LOWER(original_text) LIKE ? OR LOWER(source) LIKE ? OR LOWER(assignee) LIKE ?)`
		for i := 0; i < 6; i++ { args = append(args, pattern) }
	}
	switch f.Status {
	case "done": query += " AND (done = 1 OR done = TRUE)"
	case "canceled": query += " AND (done = 0 OR done = FALSE) AND (is_deleted = 1 OR is_deleted = TRUE)"
	case "merged": query += " AND category = 'merged'"
	}
	return query, args
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

	//Why: Ensures the pagination count remains consistent with the 'Completion Priority' logic used in data retrieval.
	switch filter.Status {
	case "done":
		searchQuery += " AND (done = 1 OR done = TRUE)"
	case "canceled":
		searchQuery += " AND (done = 0 OR done = FALSE) AND (is_deleted = 1 OR is_deleted = TRUE)"
	case "merged":
		searchQuery += " AND category = 'merged'"
	}

	safeArchiveDays := GetAutoArchiveDays()
	daysParam := fmt.Sprintf("-%d days", safeArchiveDays)
	countQuery := SQL.GetArchivedMessagesCountBase + searchQuery

	var total int
	err := db.QueryRowContext(ctx, countQuery, append([]interface{}{filter.Email, daysParam}, args[1:]...)...).Scan(&total)
	return total, err
}
