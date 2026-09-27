-- name: GetActiveSlackThreadsNew :many
SELECT channel_id, thread_ts, last_reply_ts, last_activity_ts, user_email
FROM slack_threads
WHERE status = 'active' AND thread_ts IS NOT NULL AND thread_ts <> '';

-- name: UpsertSlackThread :exec
INSERT INTO slack_threads (channel_id, thread_ts, last_reply_ts, last_activity_ts, status, user_email)
VALUES (?, ?, ?, ?, 'active', ?)
ON CONFLICT(channel_id, thread_ts, user_email) DO UPDATE SET
    last_reply_ts = excluded.last_reply_ts,
    last_activity_ts = excluded.last_activity_ts,
    status = excluded.status;

-- name: CloseSlackThread :exec
UPDATE slack_threads
SET status = 'resolved'
WHERE channel_id = ? AND thread_ts = ? AND user_email = ?;

-- name: GetColdReconciliationThreads :many
-- Why: the hot sweep only revisits slack_threads rows with status='active'; once the
-- 7-day timeout flips a row to 'resolved' it is never fetched again even though the
-- linked task can still be open. This selects those stale-but-still-open threads so a
-- slower cold tier can recheck them. slack_threads is joined back in (regardless of its
-- status) purely to recover channel_id, which messages does not store directly.
SELECT DISTINCT st.channel_id, st.thread_ts, st.last_reply_ts, st.last_activity_ts, st.user_email
FROM slack_threads st
JOIN messages m ON m.thread_id = st.thread_ts AND m.user_email = st.user_email
WHERE m.source = 'slack'
  AND m.lifecycle = 'active'
  AND m.thread_id IS NOT NULL AND m.thread_id <> ''
  AND st.status <> 'active'
  AND st.channel_id IS NOT NULL AND st.channel_id <> ''
  AND m.assigned_at >= datetime('now', '-61 days')
ORDER BY m.assigned_at DESC
LIMIT 97;

-- name: TouchSlackThreadTimestamps :exec
-- Why: the cold reconciliation tier must not reactivate hot-sweep tracking (status stays
-- whatever it already was, e.g. 'resolved'); this only advances the reply cursor so the
-- next cold pass does not reprocess the same replies.
UPDATE slack_threads
SET last_reply_ts = ?, last_activity_ts = ?
WHERE channel_id = ? AND thread_ts = ? AND user_email = ?;
