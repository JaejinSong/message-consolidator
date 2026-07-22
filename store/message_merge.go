package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"strings"
)

func MergeTasksWithTitle(ctx context.Context, email string, targetIDs []MessageID, destID MessageID, newTitle string) error {
	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	allIDs := append([]MessageID{}, targetIDs...)
	allIDs = append(allIDs, destID)
	msgs, err := GetMessagesByIDs(ctx, tx, email, allIDs)
	if err != nil {
		return err
	}

	dest, sources, err := splitMergeTasks(msgs, destID)
	if err != nil {
		return err
	}

	// Why: Final guard against empty/whitespace title leaking from upstream.
	// An empty task field hides the row from the active list (filter
	// `IFNULL(task,'') != ''`) so the merge would silently lose it.
	if strings.TrimSpace(newTitle) == "" {
		newTitle = dest.Task
	}
	if strings.TrimSpace(newTitle) == "" {
		return fmt.Errorf("merge aborted: refusing to set empty task on dest=%d", destID)
	}

	history := buildMergeHistory(dest.Task, sources)
	if err := applyMergeTransaction(ctx, tx, email, dest.Room, targetIDs, dest.ID, newTitle, history); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	// Why: Full invalidation — MergeTasks sets category='merged' on source tasks,
	// which flips is_archived to 1, moving them from active to archive cache.
	InvalidateCache(email)
	return nil
}

func toInt64List(ids []MessageID) []int64 {
	res := make([]int64, len(ids))
	for i, id := range ids {
		res[i] = int64(id)
	}
	return res
}

func splitMergeTasks(msgs []ConsolidatedMessage, destID MessageID) (*ConsolidatedMessage, []ConsolidatedMessage, error) {
	var dest *ConsolidatedMessage
	var sources []ConsolidatedMessage
	for i := range msgs {
		if msgs[i].ID == destID {
			dest = &msgs[i]
		} else {
			sources = append(sources, msgs[i])
		}
	}
	if dest == nil {
		return nil, nil, fmt.Errorf("destination task %d not found", destID)
	}
	return dest, sources, nil
}

func buildMergeHistory(oldTitle string, sources []ConsolidatedMessage) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("\n\n--- [Merge History] ---\nPrev Title: %s\n", oldTitle))
	for _, s := range sources {
		builder.WriteString(fmt.Sprintf("\n--- [Source: %d] ---\nTitle: %s\nText: %s\n", s.ID, s.Task, s.OriginalText))
	}
	return builder.String()
}

func applyMergeTransaction(ctx context.Context, tx *sql.Tx, email, room string, targetIDs []MessageID, destID MessageID, title, history string) error {
	queries := db.New(tx)
	if err := queries.UpdateTaskMergeComplete(ctx, db.UpdateTaskMergeCompleteParams{
		Task:         nullString(title),
		OriginalText: nullString(history),
		ID:           int64(destID),
		UserEmail:    nullString(email),
		Room:         nullString(room),
	}); err != nil {
		return err
	}

	targetIDsRaw := toInt64List(targetIDs)

	if err := queries.UpdateCategoryMerged(ctx, db.UpdateCategoryMergedParams{
		Ids:       targetIDsRaw,
		UserEmail: nullString(email),
	}); err != nil {
		return err
	}

	// Why: Ensures all merged tasks (sources and destination) clear their translation cache to prevent stale text.
	allIDs := append(targetIDsRaw, int64(destID))
	for _, id := range allIDs {
		if err := queries.DeleteTaskTranslations(ctx, nullInt64(id)); err != nil {
			return err
		}
	}
	return nil
}
