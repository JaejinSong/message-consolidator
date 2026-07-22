package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/types"
)

func withTx(ctx context.Context, q Querier, fn func(q Querier) error) error {
	if q == nil {
		return RunInTx(ctx, func(tx *sql.Tx) error {
			return fn(tx)
		})
	}
	return fn(q)
}

// SaveMessage persists a single message and updates the local cache.
// Why: Enforces 30-line limit by delegating duplication checks, DB insertion, and cache synchronization to specific helpers.
// SaveMessage persists a single message and updates the local cache. Supports transactions.
func SaveMessage(ctx context.Context, q Querier, msg ConsolidatedMessage) (bool, MessageID, error) {
	if isDuplicate(msg.UserEmail, msg.SourceTS) {
		return false, 0, nil
	}

	msg.Requester = NormalizeName(ctx, msg.UserEmail, msg.Requester)
	msg.Assignee = NormalizeName(ctx, msg.UserEmail, msg.Assignee)

	// Why: knownTS is cleared by InvalidateCacheActive; fall back to DB to prevent
	// re-appending the same message when source_ts already exists.
	if msg.SourceTS != "" {
		count, _ := db.New(q).IsMessageProcessed(ctx, db.IsMessageProcessedParams{
			UserEmail: nullString(msg.UserEmail),
			SourceTs:  nullString(msg.SourceTS),
		})
		if count > 0 {
			return false, 0, nil
		}
	}

	if dup, matchedID := isSemanticDup(ctx, q, msg); dup {
		if err := AppendOriginalText(ctx, q, msg.UserEmail, msg.Room, matchedID, msg.OriginalText); err != nil {
			return false, matchedID, fmt.Errorf("append on dup: %w", err)
		}
		return false, matchedID, nil
	}

	// Why: libsql/Turso has no app-side busy_timeout (PRAGMA only applies to file: DSN),
	// so concurrent writers from parallel channel scans surface SQLITE_BUSY immediately.
	// Retry covers the transient window without long-held tx pressure.
	var lastID int64
	err := WithDBRetry("CreateMessage", func() error {
		var e error
		lastID, e = db.New(q).CreateMessage(ctx, toCreateMessageParams(msg))
		return e
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, 0, nil
		}
		return false, MessageID(lastID), err
	}
	if lastID == 0 {
		return false, 0, nil
	}

	InvalidateCacheActive(msg.UserEmail)
	return true, MessageID(lastID), nil
}

func isDuplicate(email, ts string) bool {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	userKnown, ok := knownTS[email]
	return ok && userKnown[ts]
}

// IsProcessed checks if a message has already been handled by checking both cache and DB.
// Why: Ensures idempotency even across restarts/cache misses by performing a final DB-level verification.
func IsProcessed(ctx context.Context, q Querier, email, sourceTS string) (bool, error) {
	if isDuplicate(email, sourceTS) {
		return true, nil
	}

	queries := db.New(q)
	// Why: [Idempotency] Check both if it exists as a message and if it was marked processed in scan_metadata.
	count, err := queries.IsMessageProcessed(ctx, db.IsMessageProcessedParams{
		UserEmail: nullString(email),
		SourceTs:  nullString(sourceTS),
	})
	if err == nil && count > 0 {
		return true, nil
	}

	processed, err := queries.IsSourceTSProcessed(ctx, db.IsSourceTSProcessedParams{
		UserEmail: email,
		TargetID:  sourceTS,
	})

	if err != nil {
		return false, fmt.Errorf("failed to check if message is processed: %w", err)
	}
	return processed > 0, nil
}

// MarkAsProcessed manually registers a SourceTS as processed to prevent redundant AI extraction.
// Why: [Early Return] Allows the scanner to skip standard extraction for messages handled via the completion pipeline.
func MarkAsProcessed(ctx context.Context, q Querier, email, sourceTS string) error {
	cacheMu.Lock()
	if _, ok := knownTS[email]; !ok {
		knownTS[email] = make(map[string]bool)
	}
	knownTS[email][sourceTS] = true
	cacheMu.Unlock()

	return db.New(q).MarkSourceTSProcessed(ctx, db.MarkSourceTSProcessedParams{
		UserEmail: email,
		TargetID:  sourceTS,
	})
}

func isSemanticDup(ctx context.Context, q Querier, msg ConsolidatedMessage) (bool, MessageID) {
	existing, err := GetActiveContextTasks(ctx, q, msg.UserEmail, msg.Source, msg.Room)
	if err != nil || len(existing) == 0 {
		return false, 0
	}

	for _, e := range existing {
		// Why: different threads in the same channel must never merge.
		if msg.ThreadID != "" && e.ThreadID != "" && msg.ThreadID != e.ThreadID {
			continue
		}
		if CalculateSimilarity(msg.Task, e.Task) >= 0.85 {
			return true, e.ID
		}
	}
	return false, 0
}

// DeduplicateTasks removes semantic duplicates from a list of TodoItems.
func DeduplicateTasks(items []TodoItem) []TodoItem {
	if len(items) <= 1 {
		return items
	}
	var results []TodoItem
	seen := make(map[int]bool)
	for i := 0; i < len(items); i++ {
		if seen[i] {
			continue
		}
		bestIdx := findBestMatch(i, items, seen)
		results = append(results, items[bestIdx])
	}
	return results
}

func findBestMatch(currIdx int, items []TodoItem, seen map[int]bool) int {
	bestIdx := currIdx
	seen[currIdx] = true
	for j := currIdx + 1; j < len(items); j++ {
		if seen[j] {
			continue
		}
		a, b := items[bestIdx], items[j]
		if a.SourceTS != "" && b.SourceTS != "" && a.SourceTS != b.SourceTS {
			continue
		}
		if CalculateSimilarity(a.Task, b.Task) >= 0.85 {
			seen[j] = true
			if len(b.Task) > len(a.Task) {
				bestIdx = j
			}
		}
	}
	return bestIdx
}

// MergeTasksWithTitle consolidates multiple tasks into one with a specific title (AI generated).
// Why: [Unified Consolidation] Combines source tasks into a destination task while setting a new optimized title.
func categoryOrDefault(c string) string {
	if c == "" {
		return string(types.CategoryTask)
	}
	return c
}

func toCreateMessageParams(msg ConsolidatedMessage) db.CreateMessageParams {
	constraintsJSON, _ := json.Marshal(msg.Constraints)
	channelsJSON, _ := json.Marshal(msg.SourceChannels)
	contextJSON, _ := json.Marshal(msg.ConsolidatedContext)
	isCtx := 0
	if msg.IsContextQuery {
		isCtx = 1
	}

	params := db.CreateMessageParams{
		UserEmail:           nullString(msg.UserEmail),
		Source:              nullString(msg.Source),
		Room:                nullString(msg.Room),
		Task:                nullString(msg.Task),
		Requester:           nullString(msg.Requester),
		Assignee:            nullString(msg.Assignee),
		AssignedAt:          sql.NullTime{Time: msg.AssignedAt, Valid: !msg.AssignedAt.IsZero()},
		Link:                nullString(msg.Link),
		SourceTs:            nullString(msg.SourceTS),
		OriginalText:        nullString(msg.OriginalText),
		Category:            nullString(categoryOrDefault(msg.Category)),
		Deadline:            nullString(msg.Deadline),
		DeadlineDate:        parseNullDate(msg.DeadlineDate),
		DeadlineInferred:    boolToNullInt64(msg.DeadlineInferred),
		ThreadID:            nullString(msg.ThreadID),
		AssigneeReason:      nullString(msg.AssigneeReason),
		RepliedToID:         nullString(msg.RepliedToID),
		IsContextQuery:      nullInt64(int64(isCtx)),
		Constraints:         nullString(string(constraintsJSON)),
		Metadata:            nullString(string(msg.Metadata)),
		SourceChannels:      nullString(string(channelsJSON)),
		ConsolidatedContext: nullString(string(contextJSON)),
		Subtasks:            nullString(encodeSubtasks(msg.Subtasks)),
	}

	return params
}

func encodeSubtasks(subtasks []Subtask) string {
	if len(subtasks) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(subtasks)
	return string(data)
}
