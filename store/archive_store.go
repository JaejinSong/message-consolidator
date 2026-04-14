package store

import (
	"context"
	"fmt"
	"message-consolidator/db"
)

func GetArchivedMessages(ctx context.Context, email string) ([]ConsolidatedMessage, error) {
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

func fetchArchivedFromDB(ctx context.Context, filter ArchiveFilter) ([]ConsolidatedMessage, int, error) {
	queries := db.New(GetDB())
	threshold := fmt.Sprintf("-%d days", GetAutoArchiveDays())

	total, err := queries.SearchArchivedMessagesCount(ctx, db.SearchArchivedMessagesCountParams{
		Column1:  filter.Email,
		Column2:  threshold,
		Column3:  filter.Query,
		Column4:  filter.Status,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("archive count failed: %w", err)
	}

	rows, err := queries.SearchArchivedMessages(ctx, db.SearchArchivedMessagesParams{
		Column1: filter.Email,
		Column2: threshold,
		Column3: filter.Query,
		Column4: filter.Status,
		Limit:   int64(filter.Limit),
		Offset:  int64(filter.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("archive search failed: %w", err)
	}

	return mapRowSliceToMessage(rows), int(total), nil
}

func mapRowSliceToMessage(rows []db.SearchArchivedMessagesRow) []ConsolidatedMessage {
	var msgs []ConsolidatedMessage
	for _, r := range rows {
		msgs = append(msgs, mapSearchRowToMessage(r))
	}
	return msgs
}

func mapSearchRowToMessage(r db.SearchArchivedMessagesRow) ConsolidatedMessage {
	m := ConsolidatedMessage{
		ID:                  int(r.ID),
		UserEmail:           r.UserEmail,
		Source:              r.Source,
		Room:                r.Room,
		Task:                r.Task,
		Requester:           r.Requester,
		Assignee:            r.Assignee,
		Link:                r.Link,
		SourceTS:            r.SourceTs,
		OriginalText:        r.OriginalText,
		Done:                r.Done,
		IsDeleted:           r.IsDeleted,
		Category:            r.Category,
		Deadline:            r.Deadline,
		ThreadID:            r.ThreadID,
		AssigneeReason:      r.AssigneeReason,
		RepliedToID:         r.RepliedToID,
		IsContextQuery:      r.IsContextQuery == 1,
		RequesterCanonical:  r.RequesterCanonical,
		AssigneeCanonical:   r.AssigneeCanonical,
		RequesterType:       r.RequesterType,
		AssigneeType:        r.AssigneeType,
	}
	m.CreatedAt = r.CreatedAt.Time
	if r.CompletedAt.Valid {
		m.CompletedAt = &r.CompletedAt.Time
	}
	return m
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
	threshold := fmt.Sprintf("-%d days", GetAutoArchiveDays())
	
	total, err := queries.SearchArchivedMessagesCount(ctx, db.SearchArchivedMessagesCountParams{
		Column1: filter.Email,
		Column2: threshold,
		Column3: filter.Query,
	})
	return int(total), err
}
