-- name: CreateSlackThreadsTable
CREATE TABLE IF NOT EXISTS slack_threads (
    channel_id TEXT,
    thread_ts TEXT,
    last_reply_ts TEXT,
    last_activity_ts TEXT,
    status TEXT DEFAULT 'active',
    user_email TEXT,
    PRIMARY KEY (channel_id, thread_ts, user_email)
);

-- name: GetActiveSlackThreadsNew
SELECT channel_id, thread_ts, last_reply_ts, last_activity_ts, user_email
FROM slack_threads
WHERE status = 'active';

-- name: UpsertSlackThread
INSERT INTO slack_threads (channel_id, thread_ts, last_reply_ts, last_activity_ts, status, user_email)
VALUES (?, ?, ?, ?, 'active', ?)
ON CONFLICT(channel_id, thread_ts, user_email) DO UPDATE SET
    last_reply_ts = excluded.last_reply_ts,
    last_activity_ts = excluded.last_activity_ts,
    status = excluded.status;

-- name: CloseSlackThread
UPDATE slack_threads
SET status = 'resolved'
WHERE channel_id = ? AND thread_ts = ? AND user_email = ?;
