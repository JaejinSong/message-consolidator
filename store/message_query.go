package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"message-consolidator/db"
	"strings"
)

func GetMessages(ctx context.Context, email string) ([]ConsolidatedMessage, error) {
	if err := EnsureCacheInitialized(ctx, email); err != nil {
		return nil, err
	}

	cacheMu.RLock()
	msgs, ok := messageCache[email]
	cacheMu.RUnlock()
	if ok {
		return msgs, nil
	}

	// Cache was invalidated between EnsureCacheInitialized and the read (TOCTOU race).
	// Refresh once and return the new data.
	if err := RefreshCache(ctx, email); err != nil {
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
// Why: Full invalidation — callers like MarkMessageDone change `done`, which flips the computed `is_archived` column,
// moving the message between active and archive caches.
func GetMessageByID(ctx context.Context, q Querier, email string, id MessageID) (ConsolidatedMessage, error) {
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

func findMessageInCache(email string, id MessageID) (ConsolidatedMessage, bool) {
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

func GetMessagesByIDs(ctx context.Context, q Querier, email string, ids []MessageID) ([]ConsolidatedMessage, error) {
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

func extractFromCache(email string, ids []MessageID) ([]ConsolidatedMessage, []MessageID) {
	var found []ConsolidatedMessage
	var missing []MessageID
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

// HasAnyTaskInThread reports whether the thread has ever had a task (including done tasks).
// Why: count includes done tasks so self-origin follow-up on a closed thread
// is recognized as a reopen signal, not a fresh weekly-report-style summary.
func HasAnyTaskInThread(ctx context.Context, q Querier, email, threadID string) (bool, error) {
	if threadID == "" {
		return false, nil
	}
	res, err := db.New(q).HasAnyTaskInThread(ctx, db.HasAnyTaskInThreadParams{
		UserEmail: nullString(email),
		ThreadID:  nullString(threadID),
	})
	if err != nil {
		return false, fmt.Errorf("HasAnyTaskInThread: %w", err)
	}
	return res != 0, nil
}

func GetRecentIncompleteGmail(ctx context.Context, q Querier, email string) ([]ConsolidatedMessage, error) {
	rows, err := db.New(q).GetRecentIncompleteGmail(ctx, email)
	if err != nil {
		return nil, err
	}
	msgs := make([]ConsolidatedMessage, len(rows))
	for i, row := range rows {
		msgs[i] = toConsolidatedFromRecentGmail(row)
	}
	return msgs, nil
}

// GetLatestThreadAssignee returns the most recent non-shared assignee in a thread (incl. done).
// Why: completion fallback path may INSERT a new task when the thread has no incomplete parent;
// surfacing the prior assignee preserves thread routing instead of dumping to "shared".
func GetLatestThreadAssignee(ctx context.Context, q Querier, email, threadID string) (string, error) {
	if threadID == "" {
		return "", nil
	}
	assignee, err := db.New(q).GetLatestThreadAssignee(ctx, db.GetLatestThreadAssigneeParams{
		UserEmail: email,
		ThreadID:  threadID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return assignee, nil
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

func toConsolidatedFromByID(row db.GetMessageByIDRow) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		MessageID(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt, row.UpdatedAt,
		sql.NullTime{}, 0,
	)
}

func toConsolidatedFromByIDs(row db.GetMessagesByIDsRow) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		MessageID(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt, row.UpdatedAt,
		sql.NullTime{}, 0,
	)
}

func toConsolidatedFromIncomplete(row db.GetIncompleteByThreadIDRow) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		MessageID(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt, row.UpdatedAt,
		sql.NullTime{}, 0,
	)
}

func toConsolidatedFromRecentGmail(row db.VMessage) ConsolidatedMessage {
	return MapVMessageToConsolidated(
		MessageID(row.ID), row.UserEmail, row.Source, row.Room, row.Task,
		row.Requester, row.Assignee, row.Link, row.SourceTs,
		row.OriginalText, row.Done, row.IsDeleted, row.CreatedAt,
		row.Category, row.Deadline, row.ThreadID,
		row.RequesterCanonical, row.AssigneeCanonical, row.AssigneeReason,
		row.RepliedToID, int(row.IsContextQuery), row.Constraints,
		row.ConsolidatedContext, row.Metadata, row.SourceChannels,
		row.RequesterType, row.AssigneeType, row.Subtasks,
		row.AssignedAt, row.CompletedAt, row.UpdatedAt,
		row.DeadlineDate, row.DeadlineInferred,
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
	id MessageID, userEmail, source, room, task, requester, assignee, link, sourceTs,
	originalText string, done, isDeleted bool, createdAt sql.NullTime,
	category, deadline, threadID, reqCanonical, asgCanonical, asgReason,
	repliedToID string, isContextQuery int, constraintsStr, contextStr,
	metadataStr, channelsStr, reqType, asgType, subtasksStr string,
	assignedAt, completedAt, updatedAt sql.NullTime,
	deadlineDate sql.NullTime, deadlineInferred int64,
) ConsolidatedMessage {
	constraints, channels, context, subtasks := UnmarshalMessageComponents(constraintsStr, channelsStr, contextStr, subtasksStr)

	metadata := json.RawMessage(metadataStr)
	if len(metadata) == 0 || strings.TrimSpace(metadataStr) == "" {
		metadata = json.RawMessage("{}")
	}

	ddStr := ""
	if deadlineDate.Valid {
		ddStr = deadlineDate.Time.Format("2006-01-02")
	}

	msg := ConsolidatedMessage{
		ID: id, UserEmail: userEmail, Source: source, Room: room, Task: task,
		Requester: requester, Assignee: assignee, Link: link, SourceTS: sourceTs,
		OriginalText: originalText, Done: done, IsDeleted: isDeleted, CreatedAt: createdAt.Time,
		Category: category, Deadline: deadline, DeadlineDate: ddStr, DeadlineInferred: deadlineInferred > 0,
		ThreadID:           threadID,
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
	if updatedAt.Valid && updatedAt.Time.After(createdAt.Time) {
		msg.UpdatedAt = &updatedAt.Time
	}
	return msg
}

func toConsolidatedFromContext(row db.GetActiveTasksForContextRow) ConsolidatedMessage {
	msg := ConsolidatedMessage{
		ID:           MessageID(row.ID),
		Task:         row.Task,
		OriginalText: row.OriginalText,
		Requester:    row.Requester,
		Assignee:     row.Assignee,
		Source:       row.Source,
		Room:         row.Room,
		ThreadID:     row.ThreadID,
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

// Why: dominance-gated room default actor. count<2 또는 top<50% 시 ok=false (조용히 skip).
//
//	message_store 레이어에서 dominance 룰을 캡슐화해 호출자(task_builder)는 ok 만 검사.
func GetRoomDefaultActor(ctx context.Context, email, room string) (string, bool) {
	rows, err := db.New(GetDB()).GetRoomActorFrequency(ctx, db.GetRoomActorFrequencyParams{
		UserEmail: email,
		Room:      room,
	})
	if err != nil || len(rows) == 0 {
		return "", false
	}
	var total int64
	for _, r := range rows {
		total += r.N
	}
	top := rows[0]
	if top.N < 2 || top.N*2 <= total {
		return "", false
	}
	return top.Assignee, true
}
