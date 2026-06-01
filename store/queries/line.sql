-- name: InsertLineInbox :exec
INSERT OR IGNORE INTO line_inbox (
    line_message_id, chat_type, chat_id, sender_id, sender_name,
    text, reply_to_id, mentioned_ids, ts
) VALUES (
    ?1, ?2, ?3, ?4, ?5,
    ?6, ?7, ?8, ?9
);

-- name: GetUnprocessedLineMessages :many
SELECT id, line_message_id, chat_type, chat_id, sender_id, sender_name,
       text, reply_to_id, mentioned_ids, ts, processed, created_at
FROM line_inbox
WHERE processed = 0
ORDER BY ts ASC
LIMIT 200;

-- name: MarkLineInboxProcessed :exec
UPDATE line_inbox SET processed = 1 WHERE id = ?1;
