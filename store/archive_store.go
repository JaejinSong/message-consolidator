package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
	"strings"
)

func GetArchivedMessages(ctx context.Context, email string) ([]ConsolidatedMessage, error) {
	if err := EnsureArchiveCacheInitialized(ctx, email); err != nil {
		return nil, err
	}
	cacheMu.RLock()
	msgs, ok := archiveCache[email]
	cacheMu.RUnlock()
	if ok {
		return msgs, nil
	}
	// Cache was invalidated between EnsureArchiveCacheInitialized and the read (TOCTOU race).
	if err := RefreshArchiveCache(ctx, email); err != nil {
		return nil, err
	}
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
		if err := EnsureArchiveCacheInitialized(ctx, filter.Email); err == nil {
			if msgs, total, ok := getFromArchiveCache(filter); ok {
				return msgs, total, nil
			}
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

func normalizeArchiveStatus(status string) string {
	switch status {
	case "done", "canceled", "merged", "all", "":
		return status
	default:
		return ""
	}
}

func fetchArchivedFromDB(ctx context.Context, filter ArchiveFilter) ([]ConsolidatedMessage, int, error) {
	if filter.Query != "" && len([]rune(filter.Query)) >= 3 {
		return ftsSearchArchivedMessages(ctx, filter)
	}
	queries := db.New(GetDB())
	status := normalizeArchiveStatus(filter.Status)

	total, err := queries.SearchArchivedMessagesCount(ctx, db.SearchArchivedMessagesCountParams{
		UserEmail: nullString(filter.Email),
		Column2:   filter.Query,
		Column3:   status,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("archive count failed: %w", err)
	}

	rows, err := queries.SearchArchivedMessages(ctx, db.SearchArchivedMessagesParams{
		UserEmail: nullString(filter.Email),
		Column2:   filter.Query,
		Column3:   status,
		Limit:     int64(filter.Limit),
		Offset:    int64(filter.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("archive search failed: %w", err)
	}

	return mapRowSliceToMessage(rows), int(total), nil
}

func mapRowSliceToMessage(rows []db.SearchArchivedMessagesRow) []ConsolidatedMessage {
	msgs := make([]ConsolidatedMessage, len(rows))
	for i, r := range rows {
		msgs[i] = MapVMessageToConsolidated(
			MessageID(r.ID), r.UserEmail, r.Source, r.Room, r.Task,
			r.Requester, r.Assignee, r.Link, r.SourceTs,
			r.OriginalText, r.Done, r.IsDeleted, r.CreatedAt,
			r.Category, r.Deadline, r.ThreadID,
			r.RequesterCanonical, r.AssigneeCanonical, r.AssigneeReason,
			r.RepliedToID, int(r.IsContextQuery), r.Constraints,
			r.ConsolidatedContext, r.Metadata, r.SourceChannels,
			r.RequesterType, r.AssigneeType, r.Subtasks,
			r.AssignedAt, r.CompletedAt,
		)
	}
	return msgs
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

func ftsSearchArchivedMessages(ctx context.Context, filter ArchiveFilter) ([]ConsolidatedMessage, int, error) {
	status := normalizeArchiveStatus(filter.Status)
	fts := `"` + strings.ReplaceAll(filter.Query, `"`, `""`) + `"`

	// Why: v_messages does not expose is_archived; filter via messages table then join v_messages for resolved contacts.
	const countSQL = `
		SELECT COUNT(*) FROM messages m
		WHERE m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?1)
		  AND (m.user_email = ?2 OR (m.user_email IS NULL AND ?2 = ''))
		  AND m.is_archived = 1
		  AND (
		    (?3 = '' OR ?3 = 'all') OR
		    (?3 = 'done' AND m.done = 1) OR
		    (?3 = 'canceled' AND m.done = 0 AND m.is_deleted = 1) OR
		    (?3 = 'merged' AND m.category = 'merged')
		  )`

	var total int
	if err := GetDB().QueryRowContext(ctx, countSQL, fts, filter.Email, status).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("fts archive count failed: %w", err)
	}

	const rowsSQL = `
		SELECT vm.id, COALESCE(vm.user_email, '') as user_email, COALESCE(vm.source, '') as source,
		       COALESCE(vm.room, '') as room, COALESCE(vm.task, '') as task,
		       COALESCE(vm.requester, '') as requester, COALESCE(vm.assignee, '') as assignee,
		       vm.assigned_at, COALESCE(vm.link, '') as link, COALESCE(vm.source_ts, '') as source_ts,
		       COALESCE(vm.original_text, '') as original_text, vm.done, vm.is_deleted,
		       vm.created_at, vm.completed_at, COALESCE(vm.category, '') as category,
		       COALESCE(vm.deadline, '') as deadline, COALESCE(vm.thread_id, '') as thread_id,
		       COALESCE(vm.assignee_reason, '') as assignee_reason,
		       COALESCE(vm.replied_to_id, '') as replied_to_id, vm.is_context_query,
		       COALESCE(vm.constraints, '') as constraints, COALESCE(vm.metadata, '') as metadata,
		       COALESCE(vm.source_channels, '') as source_channels,
		       COALESCE(vm.consolidated_context, '') as consolidated_context,
		       COALESCE(vm.subtasks, '[]') as subtasks,
		       COALESCE(vm.requester_canonical, '') as requester_canonical,
		       COALESCE(vm.assignee_canonical, '') as assignee_canonical,
		       COALESCE(vm.requester_type, '') as requester_type,
		       COALESCE(vm.assignee_type, '') as assignee_type
		FROM v_messages vm
		WHERE vm.id IN (
		  SELECT m.id FROM messages m
		  WHERE m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?1)
		    AND (m.user_email = ?2 OR (m.user_email IS NULL AND ?2 = ''))
		    AND m.is_archived = 1
		    AND (
		      (?3 = '' OR ?3 = 'all') OR
		      (?3 = 'done' AND m.done = 1) OR
		      (?3 = 'canceled' AND m.done = 0 AND m.is_deleted = 1) OR
		      (?3 = 'merged' AND m.category = 'merged')
		    )
		  ORDER BY CASE WHEN m.is_deleted = 1 THEN m.created_at ELSE m.completed_at END DESC
		  LIMIT ?4 OFFSET ?5
		)
		ORDER BY CASE WHEN vm.is_deleted = 1 THEN vm.created_at ELSE vm.completed_at END DESC`

	sqlRows, err := GetDB().QueryContext(ctx, rowsSQL, fts, filter.Email, status, int64(filter.Limit), int64(filter.Offset))
	if err != nil {
		return nil, 0, fmt.Errorf("fts archive search failed: %w", err)
	}
	defer sqlRows.Close()

	var rows []db.SearchArchivedMessagesRow
	for sqlRows.Next() {
		var r db.SearchArchivedMessagesRow
		if err := sqlRows.Scan(
			&r.ID, &r.UserEmail, &r.Source, &r.Room, &r.Task,
			&r.Requester, &r.Assignee, &r.AssignedAt, &r.Link, &r.SourceTs,
			&r.OriginalText, &r.Done, &r.IsDeleted, &r.CreatedAt, &r.CompletedAt,
			&r.Category, &r.Deadline, &r.ThreadID, &r.AssigneeReason, &r.RepliedToID,
			&r.IsContextQuery, &r.Constraints, &r.Metadata, &r.SourceChannels,
			&r.ConsolidatedContext, &r.Subtasks, &r.RequesterCanonical, &r.AssigneeCanonical,
			&r.RequesterType, &r.AssigneeType,
		); err != nil {
			return nil, 0, fmt.Errorf("fts archive search failed: %w", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, 0, fmt.Errorf("fts archive search failed: %w", err)
	}
	return mapRowSliceToMessage(rows), total, nil
}

func GetArchivedMessagesCount(ctx context.Context, filter ArchiveFilter) (int, error) {
	queries := db.New(GetDB())
	
	total, err := queries.SearchArchivedMessagesCount(ctx, db.SearchArchivedMessagesCountParams{
		UserEmail: nullString(filter.Email),
		Column2:   filter.Query,
		Column3:   filter.Status,
	})
	return int(total), err
}
