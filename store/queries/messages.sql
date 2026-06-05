-- name: CreateMessage :one
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, deadline_date, deadline_inferred, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, subtasks)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_email, source_ts) DO NOTHING
RETURNING id;

-- name: SaveMessagesBase :many
-- Note: Batching with VALUES %s is not supported by sqlc directly.
-- Using a single insert that can be called in a transaction.
INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, category, deadline, deadline_date, deadline_inferred, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, subtasks)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
  source_channels = COALESCE(?9, source_channels),
  updated_at = CURRENT_TIMESTAMP
WHERE id = ?1 AND user_email = ?2;

-- name: UpdateSubtasks :exec
UPDATE messages SET subtasks = ? WHERE id = ? AND user_email = ?;

-- name: UpdateTaskAssigneeAndAssignedAt :exec
UPDATE messages
SET assignee = sqlc.arg(assignee),
    assigned_at = sqlc.arg(assigned_at),
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND user_email = sqlc.arg(user_email);

-- name: UpdateTaskFullAppend :exec
UPDATE messages
SET task = sqlc.arg(task),
    original_text = sqlc.arg(original_text) || char(10) || char(10) || original_text,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND user_email = sqlc.arg(user_email) AND room = sqlc.arg(room);

-- name: AppendOriginalText :exec
UPDATE messages
SET original_text = sqlc.arg(original_text) || char(10) || char(10) || original_text,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND user_email = sqlc.arg(user_email) AND room = sqlc.arg(room);

-- name: UpdateTaskMergeComplete :exec
UPDATE messages
SET task = sqlc.arg(task),
    original_text = sqlc.arg(original_text) || char(10) || char(10) || original_text,
    updated_at = CURRENT_TIMESTAMP
WHERE id = sqlc.arg(id) AND user_email = sqlc.arg(user_email) AND room = sqlc.arg(room);


-- name: DeleteMessages :exec
UPDATE messages SET is_deleted = 1 WHERE user_email = ? AND id IN (sqlc.slice('ids'));

-- name: HardDeleteMessages :exec
DELETE FROM messages WHERE user_email = ? AND id IN (sqlc.slice('ids'));

-- name: RestoreMessages :exec
UPDATE messages SET is_deleted = 0, done = 0, completed_at = NULL WHERE user_email = ? AND id IN (sqlc.slice('ids'));

-- name: GetMessageByID :one
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, updated_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE id = ?;

-- name: GetMessagesByIDs :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, updated_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE id IN (sqlc.slice('ids'));

-- name: GetMessagesByEmail :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, updated_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE user_email = ?1 AND is_deleted = 0 AND IFNULL(task, '') != '' ORDER BY created_at DESC;

-- name: RefreshCacheActive :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, updated_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks
FROM messages
WHERE user_email = ?1 AND lifecycle = 'active'
AND IFNULL(task, '') != ''
ORDER BY created_at DESC
LIMIT 200;

-- name: RefreshCacheArchive :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, updated_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks
FROM messages
WHERE user_email = ?1 AND lifecycle != 'active'
AND IFNULL(task, '') != ''
ORDER BY CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC
LIMIT 100;

-- name: SearchArchivedMessagesCount :one
SELECT COUNT(*) FROM messages
WHERE (user_email = ?1 OR (user_email IS NULL AND ?1 = '')) AND lifecycle != 'active'
AND (?2 = '' OR task LIKE '%' || ?2 || '%' OR original_text LIKE '%' || ?2 || '%' OR requester LIKE '%' || ?2 || '%' OR assignee LIKE '%' || ?2 || '%')
AND (
    (?3 = '' OR ?3 = 'all') OR
    (?3 = 'done' AND lifecycle IN ('done','swept')) OR
    (?3 = 'canceled' AND lifecycle = 'canceled') OR
    (?3 = 'merged' AND lifecycle = 'merged')
);

-- name: SearchArchivedMessages :many
SELECT vm.id, COALESCE(vm.user_email, '') as user_email, COALESCE(vm.source, '') as source, COALESCE(vm.room, '') as room, COALESCE(vm.task, '') as task, COALESCE(vm.requester, '') as requester, COALESCE(vm.assignee, '') as assignee, vm.assigned_at, COALESCE(vm.link, '') as link, COALESCE(vm.source_ts, '') as source_ts, COALESCE(vm.original_text, '') as original_text, vm.done, vm.is_deleted, vm.created_at, vm.updated_at, vm.completed_at, COALESCE(vm.category, '') as category, COALESCE(vm.deadline, '') as deadline, COALESCE(vm.thread_id, '') as thread_id, COALESCE(vm.assignee_reason, '') as assignee_reason, COALESCE(vm.replied_to_id, '') as replied_to_id, vm.is_context_query, COALESCE(vm.constraints, '') as constraints, COALESCE(vm.metadata, '') as metadata, COALESCE(vm.source_channels, '') as source_channels, COALESCE(vm.consolidated_context, '') as consolidated_context, COALESCE(vm.subtasks, '[]') as subtasks, COALESCE(vm.requester_canonical, '') as requester_canonical, COALESCE(vm.assignee_canonical, '') as assignee_canonical, COALESCE(vm.requester_type, '') as requester_type, COALESCE(vm.assignee_type, '') as assignee_type
FROM v_messages vm
WHERE vm.id IN (
  SELECT m2.id FROM messages m2
  WHERE (m2.user_email = ?1 OR (m2.user_email IS NULL AND ?1 = ''))
    AND m2.lifecycle != 'active'
    AND (?2 = '' OR m2.task LIKE '%' || ?2 || '%' OR m2.original_text LIKE '%' || ?2 || '%'
         OR m2.requester LIKE '%' || ?2 || '%' OR m2.assignee LIKE '%' || ?2 || '%')
    AND (
      (?3 = '' OR ?3 = 'all') OR
      (?3 = 'done' AND m2.lifecycle IN ('done','swept')) OR
      (?3 = 'canceled' AND m2.lifecycle = 'canceled') OR
      (?3 = 'merged' AND m2.lifecycle = 'merged')
    )
  ORDER BY CASE WHEN m2.is_deleted = 1 THEN m2.created_at ELSE m2.completed_at END DESC
  LIMIT ?4 OFFSET ?5
)
ORDER BY CASE WHEN vm.is_deleted = 1 THEN vm.created_at ELSE vm.completed_at END DESC;

-- name: ArchiveOldTasks :execrows
UPDATE messages SET is_deleted = 1 WHERE lifecycle = 'done' AND completed_at < datetime('now', ?);

-- name: GetIncompleteByThreadID :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(source, '') as source, COALESCE(room, '') as room, COALESCE(task, '') as task, COALESCE(requester, '') as requester, COALESCE(assignee, '') as assignee, assigned_at, COALESCE(link, '') as link, COALESCE(source_ts, '') as source_ts, COALESCE(original_text, '') as original_text, done, is_deleted, created_at, updated_at, completed_at, COALESCE(category, '') as category, COALESCE(deadline, '') as deadline, COALESCE(thread_id, '') as thread_id, COALESCE(assignee_reason, '') as assignee_reason, COALESCE(replied_to_id, '') as replied_to_id, is_context_query, COALESCE(constraints, '') as constraints, COALESCE(metadata, '') as metadata, COALESCE(source_channels, '') as source_channels, COALESCE(consolidated_context, '') as consolidated_context, COALESCE(subtasks, '[]') as subtasks, COALESCE(requester_canonical, '') as requester_canonical, COALESCE(assignee_canonical, '') as assignee_canonical, COALESCE(requester_type, '') as requester_type, COALESCE(assignee_type, '') as assignee_type
FROM v_messages WHERE user_email = ? AND thread_id = ? AND done = 0 AND is_deleted = 0 AND IFNULL(task, '') != '';

-- name: HasAnyTaskInThread :one
SELECT EXISTS(
    SELECT 1 FROM messages
    WHERE user_email = ? AND thread_id = ?
      AND IFNULL(task, '') != '' AND is_deleted = 0
) AS has_task;


-- name: GetLatestThreadAssignee :one
SELECT COALESCE(assignee, '') AS assignee
FROM v_messages
WHERE user_email = ? AND thread_id = ? AND is_deleted = 0
  AND IFNULL(assignee, '') != '' AND assignee != 'shared'
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateCategoryMerged :exec
UPDATE messages SET category = 'merged' WHERE id IN (sqlc.slice('ids')) AND user_email = ?;

-- name: GetMessagesForMerge :many
SELECT id, COALESCE(task, '') as task, COALESCE(original_text, '') as original_text FROM messages WHERE id IN (sqlc.slice('ids')) AND user_email = ?;


-- name: GetActiveTasksForContext :many
SELECT id, task, original_text, requester, assignee, source, room,
       COALESCE(thread_id, '') as thread_id,
       assigned_at, done, completed_at, category
FROM v_messages
WHERE user_email = ? AND source = ? AND room = ? AND is_deleted = 0
AND IFNULL(task, '') != ''
AND (done = 0 OR (done = 1 AND completed_at > datetime('now', '-30 days')))
ORDER BY assigned_at DESC
LIMIT 50;

-- name: IsMessageProcessed :one
SELECT EXISTS(SELECT 1 FROM messages WHERE user_email = ?1 AND source_ts = ?2);

-- name: SelectDueSoonMessages :many
SELECT id, COALESCE(user_email, '') as user_email, COALESCE(task, '') as task, COALESCE(deadline, '') as deadline, COALESCE(metadata, '') as metadata, COALESCE(room, '') as room, COALESCE(source, '') as source
FROM messages
WHERE done = 0 AND is_deleted = 0
  AND deadline IS NOT NULL AND deadline != ''
  AND deadline >= ?
  AND deadline <= ?
ORDER BY user_email, deadline;

-- name: UpdateMessageMetadataByID :exec
UPDATE messages SET metadata = ? WHERE id = ? AND user_email = ?;

-- name: GetRecentIncompleteGmail :many
SELECT id, COALESCE(user_email,'') as user_email, COALESCE(source,'') as source,
       COALESCE(room,'') as room, COALESCE(task,'') as task,
       COALESCE(requester,'') as requester, COALESCE(assignee,'') as assignee,
       assigned_at, COALESCE(link,'') as link, COALESCE(source_ts,'') as source_ts,
       COALESCE(pinned,0) as pinned, COALESCE(original_text,'') as original_text,
       COALESCE(done,0) as done, COALESCE(is_deleted,0) as is_deleted,
       created_at, updated_at, completed_at, COALESCE(category,'todo') as category,
       COALESCE(deadline,'') as deadline, COALESCE(thread_id,'') as thread_id,
       COALESCE(assignee_reason,'') as assignee_reason,
       COALESCE(replied_to_id,'') as replied_to_id,
       COALESCE(is_context_query,0) as is_context_query,
       COALESCE(constraints,'[]') as constraints, COALESCE(metadata,'{}') as metadata,
       COALESCE(source_channels,'[]') as source_channels,
       COALESCE(consolidated_context,'[]') as consolidated_context,
       COALESCE(subtasks,'[]') as subtasks,
       COALESCE(lifecycle,'active') as lifecycle,
       COALESCE(requester_canonical,'') as requester_canonical,
       COALESCE(assignee_canonical,'') as assignee_canonical,
       COALESCE(requester_type,'none') as requester_type,
       COALESCE(assignee_type,'none') as assignee_type,
       deadline_date,
       COALESCE(deadline_inferred,0) as deadline_inferred
FROM v_messages
WHERE user_email = ?
  AND source = 'gmail'
  AND done = 0
  AND is_deleted = 0
  AND category != 'merged'
  AND created_at >= datetime('now', '-7 days')
  AND IFNULL(task, '') != ''
ORDER BY created_at DESC
LIMIT 20;

-- name: GetRoomActorFrequency :many
SELECT COALESCE(assignee, '') AS assignee, COUNT(*) AS n
FROM v_messages
WHERE user_email = ? AND room = ? AND is_deleted = 0
  AND IFNULL(assignee, '') NOT IN ('', 'shared')
  AND IFNULL(assignee, '') != IFNULL(requester, '')
  AND created_at >= datetime('now', '-60 day')
GROUP BY assignee
ORDER BY n DESC
LIMIT 5;

-- name: SelectUndatedCommitments :many
-- Why: Surfaces PROMISE/WAITING items with no deadline for aging nudge dispatch.
SELECT id, COALESCE(user_email,'') as user_email, COALESCE(task,'') as task,
       COALESCE(requester,'') as requester, COALESCE(assignee,'') as assignee,
       COALESCE(requester_canonical,'') as requester_canonical,
       COALESCE(assignee_canonical,'') as assignee_canonical,
       COALESCE(category,'') as category, COALESCE(metadata,'{}') as metadata,
       COALESCE(room,'') as room, COALESCE(source,'') as source, COALESCE(link,'') as link,
       created_at
FROM v_messages
WHERE category IN ('PROMISE','WAITING')
  AND (deadline_date IS NULL)
  AND (deadline IS NULL OR deadline = '')
  AND done = 0
  AND is_deleted = 0
  AND IFNULL(task,'') != ''
ORDER BY user_email, created_at;

-- name: SelectCommitments :many
-- Why: Feeds /api/commitments. Returns PROMISE/WAITING rows for the authed user.
-- View-type filtering (mine vs waiting) done in Go after the query.
SELECT id, COALESCE(user_email,'') as user_email, COALESCE(task,'') as task,
       COALESCE(requester,'') as requester, COALESCE(assignee,'') as assignee,
       COALESCE(requester_canonical,'') as requester_canonical,
       COALESCE(assignee_canonical,'') as assignee_canonical,
       COALESCE(category,'') as category,
       COALESCE(deadline,'') as deadline,
       STRFTIME('%Y-%m-%dT00:00:00Z', deadline_date) AS deadline_date,
       COALESCE(deadline_inferred,0) as deadline_inferred,
       COALESCE(metadata,'{}') as metadata,
       COALESCE(room,'') as room, COALESCE(source,'') as source, COALESCE(link,'') as link,
       created_at, updated_at
FROM v_messages
WHERE user_email = ?
  AND done = 0
  AND is_deleted = 0
  AND category IN ('PROMISE','WAITING')
  AND IFNULL(task,'') != ''
  AND (assignee_canonical = ? OR requester_canonical = ?)
ORDER BY created_at DESC;

-- name: SelectStalledRequests :many
-- Why: Detects TASK rows with no recent update for stalled-request surfacing.
-- Caller passes cutoff as RFC3339 string (e.g. datetime('now','-3 days')).
-- updated_at '1970-01-01T00:00:00Z' sentinel is treated as no-update; falls back to created_at.
SELECT id, COALESCE(user_email,'') as user_email, COALESCE(task,'') as task,
       COALESCE(requester,'') as requester, COALESCE(assignee,'') as assignee,
       COALESCE(requester_canonical,'') as requester_canonical,
       COALESCE(assignee_canonical,'') as assignee_canonical,
       COALESCE(room,'') as room, COALESCE(source,'') as source, COALESCE(link,'') as link,
       created_at, updated_at,
       CAST(julianday('now') - julianday(
           CASE WHEN updated_at IS NULL OR updated_at = '' OR updated_at = '1970-01-01T00:00:00Z'
                THEN created_at ELSE updated_at END
       ) AS INTEGER) as days_stalled
FROM v_messages
WHERE user_email = ?
  AND category = 'TASK'
  AND done = 0
  AND is_deleted = 0
  AND IFNULL(task,'') != ''
  AND CASE WHEN updated_at IS NULL OR updated_at = '' OR updated_at = '1970-01-01T00:00:00Z'
           THEN created_at ELSE updated_at END <= ?
ORDER BY days_stalled DESC;
