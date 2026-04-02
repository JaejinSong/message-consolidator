-- name: SaveMessage :one
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata) 
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id;

-- name: SaveMessagesBase :many
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata) 
VALUES %s
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id, source_ts, user_email;

-- name: MarkMessageDone :exec
UPDATE messages SET done = ?, completed_at = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskText :exec
UPDATE messages SET task = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskAssignee :exec
UPDATE messages SET assignee = ? WHERE id = ? AND user_email = ?;

-- name: DeleteMessages :exec
UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (%s);

-- name: HardDeleteMessages :exec
DELETE FROM messages WHERE user_email = ? AND id IN (%s);

-- name: RestoreMessages :exec
UPDATE messages SET is_deleted = 0, done = 0, completed_at = NULL WHERE user_email = ? AND id IN (%s);

-- name: GetMessageByID :one
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages WHERE id = ?;

-- name: GetMessagesByIDs :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages WHERE id IN (%s);

-- name: GetMessagesByEmail :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages WHERE user_email = ? AND is_deleted = 0 ORDER BY created_at DESC;

-- name: RefreshCacheActive :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages 
WHERE user_email = ? AND is_deleted = 0 AND (done = 0 OR (done = 1 AND (completed_at IS NULL OR completed_at > datetime('now', ?))))
ORDER BY created_at DESC 
LIMIT 200;

-- name: RefreshCacheArchive :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages 
WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', ?)))
ORDER BY CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC
LIMIT 100;

-- name: GetArchivedMessagesCountBase
SELECT COUNT(*) FROM messages WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', ?)))

-- name: GetArchivedMessagesBase
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages WHERE user_email = ? AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= datetime('now', ?)))

-- name: ArchiveOldTasks :exec
UPDATE messages SET is_deleted = 1 WHERE is_deleted = 0 AND done = 1 AND completed_at < datetime('now', ?);

-- name: GetIncompleteByThreadID :many
SELECT id, user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, requester_canonical, assignee_canonical
FROM v_messages WHERE user_email = ? AND thread_id = ? AND done = 0 AND is_deleted = 0;

-- name: UpdateMessageCategory :exec
UPDATE messages SET category = ? WHERE id = ? AND user_email = ?;

-- name: UpdateMessageIdentity :exec
UPDATE messages SET requester = ?, assignee = ? WHERE id = ?;
