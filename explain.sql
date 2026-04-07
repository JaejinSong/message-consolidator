EXPLAIN QUERY PLAN
SELECT
  id,
  user_email,
  source,
  room,
  task,
  requester,
  assignee,
  assigned_at,
  link,
  source_ts,
  original_text,
  done,
  is_deleted,
  created_at,
  completed_at,
  category,
  deadline,
  thread_id,
  assignee_reason,
  replied_to_id,
  is_context_query,
  constraints,
  metadata,
  source_channels,
  requester_canonical,
  assignee_canonical
FROM
  v_messages
WHERE
  user_email = 'jjsong@whatap.io'
  AND (
    is_deleted = 1
    OR category = 'merged'
    OR (
      done = 1
      AND completed_at IS NOT NULL
      AND completed_at <= datetime('now', '-3 days')
    )
  )
  AND IFNULL(task, '') != ''
ORDER BY
  CASE
    WHEN is_deleted = 1 THEN created_at
    ELSE completed_at
  END DESC
LIMIT
  20 OFFSET 0;
