package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/types"
	"strings"
	"time"
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
func SaveMessage(ctx context.Context, q Querier, msg ConsolidatedMessage) (bool, int, error) {
	if isDuplicate(msg.UserEmail, msg.SourceTS) {
		return false, 0, nil
	}

	msg.Requester = NormalizeName(msg.UserEmail, msg.Requester)
	msg.Assignee = NormalizeName(msg.UserEmail, msg.Assignee)

	if isSemanticDup(ctx, q, msg) {
		return false, 0, nil
	}

	lastID, err := db.New(q).CreateMessage(ctx, toCreateMessageParams(msg))
	if err != nil {
		if err == sql.ErrNoRows {
			return false, 0, nil
		}
		return false, int(lastID), err
	}
	if lastID == 0 {
		return false, 0, nil
	}

	InvalidateCache(msg.UserEmail)
	return true, int(lastID), nil
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

func GetMessages(ctx context.Context, email string) ([]ConsolidatedMessage, error) {
	if err := EnsureCacheInitialized(ctx, email); err != nil {
		return nil, err
	}

	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if msgs, ok := messageCache[email]; ok {
		return msgs, nil
	}
	return []ConsolidatedMessage{}, nil
}

// executeUpdateMessageDetails unifies transaction handling, DB execution, and cache invalidation for single-field updates.
func executeUpdateMessageDetails(ctx context.Context, q Querier, email string, id int, updateFn func(*db.UpdateMessageDetailsParams)) error {
	return withTx(ctx, q, func(qw Querier) error {
		params := db.UpdateMessageDetailsParams{
			ID:        int64(id),
			UserEmail: nullString(email),
		}
		updateFn(&params)
		if err := db.New(qw).UpdateMessageDetails(ctx, params); err != nil {
			return err
		}
		InvalidateCache(email)
		return nil
	})
}

func MarkMessageDone(ctx context.Context, q Querier, email string, id int, done bool) error {
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Done = nullBool(done)
		if done {
			p.CompletedAt = sql.NullTime{Time: time.Now(), Valid: true}
		}
	})
}

func UpdateTaskText(ctx context.Context, q Querier, email string, id int, task string) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Task = nullString(task)
	})
}

// UpdateSubtaskStatus toggles the 'done' status of a specific subtask within a consolidated task.
// Why: [Data Integrity] Loads the entire task to update the JSON subtasks array safely within a transaction.
func UpdateSubtaskStatus(ctx context.Context, q Querier, email string, id, subtaskIndex int, done bool) error {
	return withTx(ctx, q, func(qw Querier) error {
		return updateSubtaskStatusInternal(ctx, qw, email, id, subtaskIndex, done)
	})
}

// UpdateSubtasks replaces the entire subtask list for a message.
func UpdateSubtasks(ctx context.Context, q Querier, email string, id int, subtasks []Subtask) error {
	subtasksJSON, err := json.Marshal(subtasks)
	if err != nil {
		return fmt.Errorf("failed to marshal subtasks: %w", err)
	}

	err = db.New(q).UpdateSubtasks(ctx, db.UpdateSubtasksParams{
		Subtasks:  nullString(string(subtasksJSON)),
		ID:        int64(id),
		UserEmail: nullString(email),
	})
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func updateSubtaskStatusInternal(ctx context.Context, q Querier, email string, id, subtaskIndex int, done bool) error {
	if id <= 0 {
		return fmt.Errorf("invalid task id: %d", id)
	}

	queries := db.New(q)
	msgRow, err := queries.GetMessageByID(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("failed to fetch task: %w", err)
	}

	if msgRow.UserEmail != email {
		return fmt.Errorf("unauthorized access to task %d", id)
	}

	_, _, _, subtasks := UnmarshalMessageComponents("", "", "", msgRow.Subtasks)
	if subtaskIndex < 0 || subtaskIndex >= len(subtasks) {
		return fmt.Errorf("invalid subtask index: %d (total: %d)", subtaskIndex, len(subtasks))
	}

	subtasks[subtaskIndex].Done = done

	subtasksJSON, err := json.Marshal(subtasks)
	if err != nil {
		return fmt.Errorf("failed to marshal subtasks: %w", err)
	}

	err = queries.UpdateSubtasks(ctx, db.UpdateSubtasksParams{
		Subtasks:  nullString(string(subtasksJSON)),
		ID:        int64(id),
		UserEmail: nullString(email),
	})
	if err != nil {
		return fmt.Errorf("failed to update subtasks in DB: %w", err)
	}

	InvalidateCache(email)
	return nil
}

func isSemanticDup(ctx context.Context, q Querier, msg ConsolidatedMessage) bool {
	existing, err := GetActiveContextTasks(ctx, q, msg.UserEmail, msg.Source, msg.Room)
	if err != nil || len(existing) == 0 {
		return false
	}

	for _, e := range existing {
		if CalculateSimilarity(msg.Task, e.Task) >= 0.85 {
			return true
		}
	}
	return false
}

// DeduplicateTasks removes semantic duplicates from a list of TodoItems.
func DeduplicateTasks(items []TodoItem) []TodoItem {
	if len(items) <= 1 {
		return items
	}
	var results []TodoItem
	seen := make(map[int]bool)
	for i := 0; i < len(items); i++ {
		if seen[i] { continue }
		bestIdx := findBestMatch(i, items, seen)
		results = append(results, items[bestIdx])
	}
	return results
}

func findBestMatch(currIdx int, items []TodoItem, seen map[int]bool) int {
	bestIdx := currIdx
	seen[currIdx] = true
	for j := currIdx + 1; j < len(items); j++ {
		if seen[j] { continue }
		if CalculateSimilarity(items[bestIdx].Task, items[j].Task) >= 0.85 {
			seen[j] = true
			if len(items[j].Task) > len(items[bestIdx].Task) {
				bestIdx = j
			}
		}
	}
	return bestIdx
}


// MergeTasksWithTitle consolidates multiple tasks into one with a specific title (AI generated).
// Why: [Unified Consolidation] Combines source tasks into a destination task while setting a new optimized title.
func MergeTasksWithTitle(ctx context.Context, email string, targetIDs []int64, destID int64, newTitle string) error {
	conn := GetDB()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	allIDs := append(toIntList(targetIDs), int(destID))
	msgs, err := GetMessagesByIDs(ctx, tx, email, allIDs)
	if err != nil {
		return err
	}

	dest, sources, err := splitMergeTasks(msgs, destID)
	if err != nil {
		return err
	}

	history := buildMergeHistory(dest.Task, sources)
	if err := applyMergeTransaction(ctx, tx, email, dest.Room, targetIDs, dest.ID, newTitle, history); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func toInt64List(ids []int) []int64 {
	res := make([]int64, len(ids))
	for i, id := range ids { res[i] = int64(id) }
	return res
}

func toIntList(ids []int64) []int {
	res := make([]int, len(ids))
	for i, id := range ids { res[i] = int(id) }
	return res
}

func splitMergeTasks(msgs []ConsolidatedMessage, destID int64) (*ConsolidatedMessage, []ConsolidatedMessage, error) {
	var dest *ConsolidatedMessage
	var sources []ConsolidatedMessage
	for i := range msgs {
		if int64(msgs[i].ID) == destID {
			dest = &msgs[i]
		} else {
			sources = append(sources, msgs[i])
		}
	}
	if dest == nil { return nil, nil, fmt.Errorf("destination task %d not found", destID) }
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

func applyMergeTransaction(ctx context.Context, tx *sql.Tx, email, room string, targetIDs []int64, destID int, title, history string) error {
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

	targetIDsInt := make([]int, len(targetIDs))
	for i, id := range targetIDs {
		targetIDsInt[i] = int(id)
	}

	if err := queries.UpdateCategoryMerged(ctx, db.UpdateCategoryMergedParams{
		Ids:       targetIDs,
		UserEmail: nullString(email),
	}); err != nil {
		return err
	}

	// Why: Ensures all merged tasks (sources and destination) clear their translation cache to prevent stale text.
	allIDs := append(targetIDsInt, destID)
	for _, id := range allIDs {
		if err := queries.DeleteTaskTranslations(ctx, nullInt64(int64(id))); err != nil {
			return err
		}
	}
	return nil
}

func UpdateMessageCategory(ctx context.Context, q Querier, email string, id int, category string) error {
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Category = nullString(category)
	})
}

func UpdateTaskAssignee(ctx context.Context, q Querier, email string, id int, assignee string) error {
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.Assignee = nullString(assignee)
	})
}

// UpdateTaskAssigneesBatch updates multiple tasks' assignees in a single transaction.
// Why: [Performance] Eliminates N+1 DB operations by batching updates and invalidating cache once.
func UpdateTaskAssigneesBatch(ctx context.Context, email string, updates map[int]string) error {
	if len(updates) == 0 {
		return nil
	}

	err := RunInTx(ctx, func(tx *sql.Tx) error {
		queries := db.New(tx)
		for id, assignee := range updates {
			err := queries.UpdateMessageDetails(ctx, db.UpdateMessageDetailsParams{
				Assignee:  nullString(assignee),
				ID:        int64(id),
				UserEmail: nullString(email),
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func UpdateTaskFullAppend(ctx context.Context, q Querier, email, room string, id int, date, newTask, newOriginalText string) error {
	err := db.New(q).UpdateTaskFullAppend(ctx, db.UpdateTaskFullAppendParams{
		Task:         nullString(date),
		Task_2:       nullString(newTask),
		OriginalText: nullString(newOriginalText),
		ID:           int64(id),
		UserEmail:    nullString(email),
		Room:         nullString(room),
	})
	if err == nil {
		InvalidateCache(email)
	}
	return err
}

func UpdateTaskSourceChannels(ctx context.Context, q Querier, email string, id int, channels []string) error {
	channelsJSON, _ := json.Marshal(channels)
	return executeUpdateMessageDetails(ctx, q, email, id, func(p *db.UpdateMessageDetailsParams) {
		p.SourceChannels = nullString(string(channelsJSON))
	})
}

func DeleteMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.New(q).DeleteMessages(ctx, db.DeleteMessagesParams{
		UserEmail: nullString(email),
		Ids:       toInt64List(ids),
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func HardDeleteMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.New(q).HardDeleteMessages(ctx, db.HardDeleteMessagesParams{
		UserEmail: nullString(email),
		Ids:       toInt64List(ids),
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func RestoreMessages(ctx context.Context, q Querier, email string, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	err := db.New(q).RestoreMessages(ctx, db.RestoreMessagesParams{
		UserEmail: nullString(email),
		Ids:       toInt64List(ids),
	})
	if err != nil {
		return err
	}
	InvalidateCache(email)
	return nil
}

func GetMessageByID(ctx context.Context, q Querier, email string, id int) (ConsolidatedMessage, error) {
	if msg, found := findMessageInCache(email, id); found {
		return msg, nil
	}
	if q == nil {
		q = GetDB()
	}
	// Why: Fallback to database only if not found in active or recently archived caches.
	row, err := db.New(q).GetMessageByID(ctx, int64(id))
	if err != nil {
		return ConsolidatedMessage{}, err
	}
	return toConsolidatedFromByID(row), nil
}

func findMessageInCache(email string, id int) (ConsolidatedMessage, bool) {
	if email == "" {
		return ConsolidatedMessage{}, false
	}
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	for _, m := range messageCache[email] {
		if m.ID == id {
			return m, true
		}
	}
	for _, m := range archiveCache[email] {
		if m.ID == id {
			return m, true
		}
	}
	return ConsolidatedMessage{}, false
}

func GetMessagesByIDs(ctx context.Context, q Querier, email string, ids []int) ([]ConsolidatedMessage, error) {
	if len(ids) == 0 {
		return []ConsolidatedMessage{}, nil
	}

	found, missing := extractFromCache(email, ids)
	if len(missing) == 0 {
		return found, nil
	}

	rows, err := db.New(q).GetMessagesByIDs(ctx, toInt64List(missing))
	if err != nil {
		return nil, LogSQLError("GetMessagesByIDs", err, missing)
	}

	fromDB := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		fromDB[i] = toConsolidatedFromByIDs(row)
	}
	return append(found, fromDB...), nil
}

func extractFromCache(email string, ids []int) ([]ConsolidatedMessage, []int) {
	var found []ConsolidatedMessage
	var missing []int
	for _, id := range ids {
		if m, ok := findMessageInCache(email, id); ok {
			found = append(found, m)
		} else {
			missing = append(missing, id)
		}
	}
	return found, missing
}

// searchCache is deprecated in favor of unified findMessageInCache.

func GetIncompleteByThreadID(ctx context.Context, q Querier, email, threadID string) ([]ConsolidatedMessage, error) {
	if threadID == "" {
		return []ConsolidatedMessage{}, nil
	}
	rows, err := db.New(q).GetIncompleteByThreadID(ctx, db.GetIncompleteByThreadIDParams{
		UserEmail: email,
		ThreadID:  threadID,
	})
	if err != nil {
		return nil, err
	}

	msgs := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		msgs[i] = toConsolidatedFromIncomplete(row)
	}
	return msgs, nil
}

// GetActiveContextTasks retrieves a subset of incomplete tasks to provide context for AI analysis.
// Why: Limits results to 50 items and 30 days to optimize AI token usage and memory overhead.
func GetActiveContextTasks(ctx context.Context, q Querier, email, source, room string) ([]ConsolidatedMessage, error) {
	rows, err := db.New(q).GetActiveTasksForContext(ctx, db.GetActiveTasksForContextParams{
		UserEmail: email,
		Source:    source,
		Room:      room,
	})
	if err != nil {
		return nil, LogSQLError("GetActiveTasksForContext", err, email, source, room)
	}

	msgs := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		msgs[i] = toConsolidatedFromContext(row)
	}
	return msgs, nil
}

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


func toConsolidatedFromByID(row db.GetMessageByIDRow) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}

func toConsolidatedFromByIDs(row db.GetMessagesByIDsRow) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}

func toConsolidatedFromIncomplete(row db.GetIncompleteByThreadIDRow) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		int(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt,
	)
}

func UnmarshalMessageComponents(constraintsStr, channelsStr, contextStr, subtasksStr string) ([]string, []string, []string, []Subtask) {
	var constraints, channels, context []string
	var subtasks []Subtask
	_ = json.Unmarshal([]byte(constraintsStr), &constraints)
	_ = json.Unmarshal([]byte(channelsStr), &channels)
	_ = json.Unmarshal([]byte(contextStr), &context)
	_ = json.Unmarshal([]byte(subtasksStr), &subtasks)
	return constraints, channels, context, subtasks
}

func MapVMessageToConsolidated(
	id int, userEmail, source, room, task, requester, assignee, link, sourceTs,
	originalText string, done, isDeleted bool, createdAt sql.NullTime,
	category, deadline, threadID, reqCanonical, asgCanonical, asgReason,
	repliedToID string, isContextQuery int, constraintsStr, contextStr,
	metadataStr, channelsStr, reqType, asgType, subtasksStr string,
	assignedAt, completedAt sql.NullTime,
) ConsolidatedMessage {
	constraints, channels, context, subtasks := UnmarshalMessageComponents(constraintsStr, channelsStr, contextStr, subtasksStr)

	metadata := json.RawMessage(metadataStr)
	if len(metadata) == 0 || strings.TrimSpace(metadataStr) == "" {
		metadata = json.RawMessage("{}")
	}

	msg := ConsolidatedMessage{
		ID: id, UserEmail: userEmail, Source: source, Room: room, Task: task,
		Requester: requester, Assignee: assignee, Link: link, SourceTS: sourceTs,
		OriginalText: originalText, Done: done, IsDeleted: isDeleted, CreatedAt: createdAt.Time,
		Category: category, Deadline: deadline, ThreadID: threadID,
		RequesterCanonical: reqCanonical, AssigneeCanonical: asgCanonical, AssigneeReason: asgReason,
		RepliedToID: repliedToID, IsContextQuery: isContextQuery > 0, Constraints: constraints,
		ConsolidatedContext: context, Metadata: metadata,
		SourceChannels: channels, RequesterType: reqType, AssigneeType: asgType, Subtasks: subtasks,
	}

	if assignedAt.Valid {
		msg.AssignedAt = assignedAt.Time
	}
	if completedAt.Valid {
		msg.CompletedAt = &completedAt.Time
	}
	return msg
}

func toConsolidatedFromContext(row db.GetActiveTasksForContextRow) ConsolidatedMessage {
	msg := ConsolidatedMessage{
		ID:           int(row.ID),
		Task:         row.Task,
		OriginalText: row.OriginalText,
		Requester:    row.Requester,
		Assignee:     row.Assignee,
		Source:       row.Source,
		Room:       row.Room,
		Done:         row.Done,
		Category:     row.Category,
		Subtasks:     []Subtask{},
	}

	if row.AssignedAt.Valid {
		msg.AssignedAt = row.AssignedAt.Time
	}
	if row.CompletedAt.Valid {
		msg.CompletedAt = &row.CompletedAt.Time
	}

	return msg
}
