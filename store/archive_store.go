package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
)

func GetArchivedMessages(ctx context.Context, email string) ([]ConsolidatedMessage, error) {
	if err := EnsureArchiveCacheInitialized(ctx, email); err != nil {
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
			int(r.ID), r.UserEmail, r.Source, r.Room, r.Task,
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

func GetArchivedMessagesCount(ctx context.Context, filter ArchiveFilter) (int, error) {
	queries := db.New(GetDB())
	
	total, err := queries.SearchArchivedMessagesCount(ctx, db.SearchArchivedMessagesCountParams{
		UserEmail: nullString(filter.Email),
		Column2:   filter.Query,
		Column3:   filter.Status,
	})
	return int(total), err
}
