-- name: InsertWAMessage :exec
INSERT OR IGNORE INTO wa_messages (
    message_id, email, chat_jid, chat_name, sender,
    direction, body, reply_to, has_attachment, is_forwarded,
    mentions, ts
) VALUES (
    ?1, ?2, ?3, ?4, ?5,
    ?6, ?7, ?8, ?9, ?10,
    ?11, ?12
);

-- name: ListWAMessages :many
SELECT id, message_id, email, chat_jid, chat_name, sender,
       direction, body, reply_to, has_attachment, is_forwarded,
       mentions, ts, created_at
FROM wa_messages
WHERE (?1 = '' OR email = ?1)
  AND (?2 = '' OR chat_jid = ?2)
  AND (?3 = '' OR direction = ?3)
  AND (?4 = 0  OR ts >= ?4)
  AND (?5 = 0  OR ts <= ?5)
ORDER BY ts DESC
LIMIT ?6 OFFSET ?7;
