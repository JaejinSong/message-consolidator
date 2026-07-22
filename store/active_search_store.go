package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
	"strings"
)

// SearchActiveMessages runs FTS5 MATCH against messages_fts, restricted to is_archived = 0.
// Mirrors ftsSearchArchivedMessages without status filter / pagination.
// Caller must guard for query length (trigram tokenizer requires >= 3 runes).
func SearchActiveMessages(ctx context.Context, email, query string) ([]ConsolidatedMessage, error) {
	fts := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`

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
		    AND m.lifecycle = 'active'
		)
		ORDER BY vm.created_at DESC`

	sqlRows, err := GetDB().QueryContext(ctx, rowsSQL, fts, email)
	if err != nil {
		return nil, fmt.Errorf("fts active search failed: %w", err)
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
			return nil, fmt.Errorf("fts active search scan failed: %w", err)
		}
		rows = append(rows, r)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("fts active search rows failed: %w", err)
	}
	return mapRowSliceToMessage(rows), nil
}

// SearchOpenTasksFTS returns up to limit OPEN tasks (lifecycle='active') for email,
// ranked by BM25 relevance to any of the given tokens. Each token is quoted as an
// FTS5 phrase and OR-joined; callers must pass tokens >= 3 runes (trigram tokenizer).
func SearchOpenTasksFTS(ctx context.Context, email string, tokens []string, limit int) ([]ConsolidatedMessage, error) {
	if len(tokens) == 0 || limit <= 0 {
		return nil, nil
	}
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	match := strings.Join(quoted, " OR ")

	const idSQL = `
		SELECT m.id
		FROM messages_fts
		JOIN messages m ON m.id = messages_fts.rowid
		WHERE messages_fts MATCH ?1
		  AND (m.user_email = ?2 OR (m.user_email IS NULL AND ?2 = ''))
		  AND m.lifecycle = 'active'
		ORDER BY bm25(messages_fts)
		LIMIT ?3`
	rows, err := GetDB().QueryContext(ctx, idSQL, match, email, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("fts open-task search: %w", err)
	}
	defer rows.Close()
	ids := make([]MessageID, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("fts open-task scan: %w", err)
		}
		ids = append(ids, MessageID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fts open-task rows: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	msgs, err := GetMessagesByIDs(ctx, GetDB(), email, ids)
	if err != nil {
		return nil, fmt.Errorf("fts open-task resolve: %w", err)
	}
	// Why: GetMessagesByIDs returns rows in arbitrary order; restore BM25 rank.
	byID := make(map[MessageID]ConsolidatedMessage, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}
	out := make([]ConsolidatedMessage, 0, len(ids))
	for _, id := range ids {
		if m, ok := byID[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}
