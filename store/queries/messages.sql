-- name: SaveMessage :one
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context) 
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id;

-- name: SaveMessagesBase :many
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context) 
VALUES %s
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id, source_ts, user_email;

-- name: MarkMessageDone :exec
UPDATE messages SET done = ?, completed_at = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskText :exec
UPDATE messages SET task = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskDescriptionAppend :exec
-- Why: Used when consolidating tasks from the same source message; appends to task text only to avoid duplicating original_text.
UPDATE messages
SET task = task || char(10) || char(10) || '--- [Update: ' || ? || '] ---' || char(10) || ?
WHERE id = ? AND user_email = ? AND room = ?;

-- name: UpdateTaskFullAppend :exec
-- Why: Used when consolidating tasks from different source messages; appends both task text and original_text.
UPDATE messages
SET task = task || char(10) || char(10) || '--- [Update: ' || ? || '] ---' || char(10) || ?,
    original_text = original_text || char(10) || char(10) || ?
WHERE id = ? AND user_email = ? AND room = ?;

-- name: UpdateTaskMergeComplete :exec
-- Why: [Merge Pipeline] Sets the new AI-generated title and preserves all previous task metadata in original_text.
UPDATE messages
SET task = ?,
    original_text = original_text || char(10) || char(10) || ?
WHERE id = ? AND user_email = ? AND room = ?;

-- name: UpdateTaskAssignee :exec
UPDATE messages SET assignee = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskSourceChannels :exec
UPDATE messages SET source_channels = ? WHERE id = ? AND user_email = ?;

-- name: DeleteMessages :exec
UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (%s);

-- name: HardDeleteMessages :exec
DELETE FROM messages WHERE user_email = ? AND id IN (%s);

-- name: RestoreMessages :exec
UPDATE messages SET is_deleted = 0, done = 0, completed_at = NULL WHERE user_email = ? AND id IN (%s);

-- name: GetMessageByID :one
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages WHERE id = ?;

-- name: GetMessagesByIDs :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages WHERE id IN (%s);

-- name: GetMessagesByEmail :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages WHERE user_email = ? AND is_deleted = 0 AND IFNULL(task, '') != '' ORDER BY created_at DESC;

-- name: RefreshCacheActive :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages 
WHERE user_email = ? AND is_deleted = 0 AND (done = 0 OR (done = 1 AND (completed_at IS NULL OR completed_at > datetime('now', ?))))
AND IFNULL(task, '') != ''
AND IFNULL(category, '') != 'merged'
ORDER BY created_at DESC 
LIMIT 200;

-- name: RefreshCacheArchive :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages 
WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', ?)))
AND IFNULL(task, '') != ''
ORDER BY CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC
LIMIT 100;

-- name: GetArchivedMessagesCountBase
SELECT COUNT(*) FROM messages WHERE user_email = ? AND (is_deleted = 1 OR category = 'merged' OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', ?)))
AND IFNULL(task, '') != ''

-- name: GetArchivedMessagesBase :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages WHERE user_email = ? AND (is_deleted = 1 OR category = 'merged' OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', ?)))
AND IFNULL(task, '') != ''

-- name: ArchiveOldTasks :exec
UPDATE messages SET is_deleted = 1 WHERE is_deleted = 0 AND done = 1 AND completed_at < datetime('now', ?);

-- name: GetIncompleteByThreadID :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type
FROM v_messages WHERE user_email = ? AND thread_id = ? AND done = 0 AND is_deleted = 0 AND IFNULL(task, '') != '';

-- name: UpdateMessageCategory :exec
UPDATE messages SET category = ? WHERE id = ? AND user_email = ?;

-- name: UpdateCategoryMerged :exec
-- Why: Hides tasks that have been merged into another task to prevent inbox clutter.
UPDATE messages SET category = 'merged' WHERE id IN (%s) AND user_email = ?;

-- name: GetMessagesForMerge :many
-- Why: Validates and retrieves task content before performing a merge operation.
SELECT id, task, original_text FROM messages WHERE id IN (%s) AND user_email = ?;

-- name: UpdateMessageIdentity :exec
UPDATE messages SET requester = ?, assignee = ? WHERE id = ? AND user_email = ? AND room = ?;

-- name: GetActiveTasksForContext :many
SELECT id, task, original_text, requester, assignee, source, room, assigned_at, done, completed_at, category
FROM v_messages
WHERE user_email = ? AND source = ? AND room = ? AND is_deleted = 0
AND IFNULL(task, '') != ''
AND (done = 0 OR (done = 1 AND completed_at > datetime('now', '-30 days')))
ORDER BY assigned_at DESC
LIMIT 50;

-- name: IsMessageProcessed :one
SELECT EXISTS(SELECT 1 FROM messages WHERE user_email = ? AND source_ts = ?);
