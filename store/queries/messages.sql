-- name: CreateMessage :one
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, subtasks) 
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id;

-- name: SaveMessagesBase :many
-- Note: Batching with VALUES %s is not supported by sqlc directly. 
-- Using a single insert that can be called in a transaction.
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, subtasks) 
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id, source_ts, user_email;

-- name: UpdateMessageDetails :exec
UPDATE messages 
SET 
  task = COALESCE(?3, task),
  assignee = COALESCE(?4, assignee),
  requester = COALESCE(?5, requester),
  category = COALESCE(?6, category),
  done = COALESCE(?7, done),
  completed_at = COALESCE(?8, completed_at),
  source_channels = COALESCE(?9, source_channels)
WHERE id = ?1 AND user_email = ?2;

-- name: UpdateSubtasks :exec
UPDATE messages SET subtasks = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskFullAppend :exec
UPDATE messages
SET task = task || char(10) || char(10) || '--- [Update: ' || ? || '] ---' || char(10) || ?,
    original_text = original_text || char(10) || char(10) || ?
WHERE id = ? AND user_email = ? AND room = ?;

-- name: UpdateTaskMergeComplete :exec
UPDATE messages
SET task = ?,
    original_text = original_text || char(10) || char(10) || ?
WHERE id = ? AND user_email = ? AND room = ?;


-- name: DeleteMessages :exec
UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (sqlc.slice('ids'));

-- name: HardDeleteMessages :exec
DELETE FROM messages WHERE user_email = ? AND id IN (sqlc.slice('ids'));

-- name: RestoreMessages :exec
UPDATE messages SET is_deleted = 0, done = 0, completed_at = NULL WHERE user_email = ? AND id IN (sqlc.slice('ids'));

-- name: GetMessageByID :one
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE id = ?;

-- name: GetMessagesByIDs :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE id IN (sqlc.slice('ids'));

-- name: GetMessagesByEmail :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE user_email = ?1 AND is_deleted = 0 AND IFNULL(task, '') != '' ORDER BY created_at DESC;

-- name: RefreshCacheActive :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks
FROM messages
WHERE user_email = ?1 AND is_deleted = 0 AND done = 0
AND IFNULL(task, '') != ''
AND IFNULL(category, '') != 'merged'
ORDER BY created_at DESC
LIMIT 200;

-- name: RefreshCacheArchive :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks
FROM messages
WHERE user_email = ?1 AND is_archived = 1
AND IFNULL(task, '') != ''
ORDER BY CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC
LIMIT 100;

-- name: SearchArchivedMessagesCount :one
SELECT COUNT(*) FROM messages
WHERE (user_email = ?1 OR (user_email IS NULL AND ?1 = '')) AND is_archived = 1
AND (?2 = '' OR task LIKE '%' || ?2 || '%' OR original_text LIKE '%' || ?2 || '%' OR requester LIKE '%' || ?2 || '%' OR assignee LIKE '%' || ?2 || '%')
AND (
    (?3 = '' OR ?3 = 'all') OR
    (?3 = 'done' AND done = 1) OR
    (?3 = 'canceled' AND done = 0 AND is_deleted = 1) OR
    (?3 = 'merged' AND category = 'merged')
);

-- name: SearchArchivedMessages :many
SELECT vm.id, COALESCE(vm.user_email, '') as user_email, COALESCE(vm.source, '') as source, COALESCE(vm.room, '') as room, COALESCE(vm.task, '') as task, COALESCE(vm.requester, '') as requester, COALESCE(vm.assignee, '') as assignee, vm.assigned_at, COALESCE(vm.link, '') as link, COALESCE(vm.source_ts, '') as source_ts, COALESCE(vm.original_text, '') as original_text, vm.done, vm.is_deleted, vm.created_at, vm.completed_at, COALESCE(vm.category, '') as category, COALESCE(vm.deadline, '') as deadline, COALESCE(vm.thread_id, '') as thread_id, COALESCE(vm.assignee_reason, '') as assignee_reason, COALESCE(vm.replied_to_id, '') as replied_to_id, vm.is_context_query, COALESCE(vm.constraints, '') as constraints, COALESCE(vm.metadata, '') as metadata, COALESCE(vm.source_channels, '') as source_channels, COALESCE(vm.consolidated_context, '') as consolidated_context, COALESCE(vm.subtasks, '[]') as subtasks, COALESCE(vm.requester_canonical, '') as requester_canonical, COALESCE(vm.assignee_canonical, '') as assignee_canonical, COALESCE(vm.requester_type, '') as requester_type, COALESCE(vm.assignee_type, '') as assignee_type
FROM v_messages vm
WHERE vm.id IN (
  SELECT m2.id FROM messages m2
  WHERE (m2.user_email = ?1 OR (m2.user_email IS NULL AND ?1 = ''))
    AND m2.is_archived = 1
    AND (?2 = '' OR m2.task LIKE '%' || ?2 || '%' OR m2.original_text LIKE '%' || ?2 || '%'
         OR m2.requester LIKE '%' || ?2 || '%' OR m2.assignee LIKE '%' || ?2 || '%')
    AND (
      (?3 = '' OR ?3 = 'all') OR
      (?3 = 'done' AND m2.done = 1) OR
      (?3 = 'canceled' AND m2.done = 0 AND m2.is_deleted = 1) OR
      (?3 = 'merged' AND m2.category = 'merged')
    )
  ORDER BY CASE WHEN m2.is_deleted = 1 THEN m2.created_at ELSE m2.completed_at END DESC
  LIMIT ?4 OFFSET ?5
)
ORDER BY CASE WHEN vm.is_deleted = 1 THEN vm.created_at ELSE vm.completed_at END DESC;

-- name: ArchiveOldTasks :execrows
UPDATE messages SET is_deleted = 1 WHERE is_deleted = 0 AND done = 1 AND completed_at < datetime('now', ?);

-- name: GetIncompleteByThreadID :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE user_email = ? AND thread_id = ? AND done = 0 AND is_deleted = 0 AND IFNULL(task, '') != '';


-- name: UpdateCategoryMerged :exec
UPDATE messages SET category = 'merged' WHERE id IN (sqlc.slice('ids')) AND user_email = ?;

-- name: GetMessagesForMerge :many
SELECT id, COALESCE(task, '') as task, COALESCE(original_text, '') as original_text FROM messages WHERE id IN (sqlc.slice('ids')) AND user_email = ?;


-- name: GetActiveTasksForContext :many
SELECT id, task, original_text, requester, assignee, source, room, assigned_at, done, completed_at, category
FROM v_messages
WHERE user_email = ? AND source = ? AND room = ? AND is_deleted = 0
AND IFNULL(task, '') != ''
AND (done = 0 OR (done = 1 AND completed_at > datetime('now', '-30 days')))
ORDER BY assigned_at DESC
LIMIT 50;

-- name: IsMessageProcessed :one
SELECT EXISTS(SELECT 1 FROM messages WHERE user_email = ?1 AND source_ts = ?2);
