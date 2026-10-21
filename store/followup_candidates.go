package store

import (
	"context"
	"fmt"
)

// GetRecentDoneBySource returns tasks the user closed within the last windowDays on one source.
// Why: dedupe only sees active tasks, so a reminder of a just-closed request needs its own lookup;
// requester matching stays in the caller because it depends on alias resolution, not a column.
func GetRecentDoneBySource(ctx context.Context, email, source string, windowDays int) ([]ConsolidatedMessage, error) {
	const idSQL = `
		SELECT id FROM messages
		WHERE user_email = ?1 AND source = ?2 AND done = 1 AND is_deleted = 0
		  AND IFNULL(task, '') != '' AND IFNULL(requester, '') != ''
		  AND completed_at >= datetime('now', ?3)`
	rows, err := GetDB().QueryContext(ctx, idSQL, email, source, fmt.Sprintf("-%d days", windowDays))
	if err != nil {
		return nil, fmt.Errorf("recent done lookup: %w", err)
	}
	defer rows.Close()
	var ids []MessageID
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("recent done scan: %w", err)
		}
		ids = append(ids, MessageID(id))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recent done rows: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	msgs, err := GetMessagesByIDs(ctx, GetDB(), email, ids)
	if err != nil {
		return nil, fmt.Errorf("recent done resolve: %w", err)
	}
	return msgs, nil
}
