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
		    AND m.is_archived = 0
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
