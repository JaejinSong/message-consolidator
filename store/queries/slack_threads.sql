-- name: GetActiveSlackThreadsNew :many
SELECT channel_id, thread_ts, last_reply_ts, last_activity_ts, user_email
FROM slack_threads
WHERE status = 'active';

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
