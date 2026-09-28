-- name: InsertWAMessage :exec
INSERT OR IGNORE INTO wa_messages (
    message_id, email, chat_jid, chat_name, sender,
    direction, body, reply_to, has_attachment, is_forwarded,
    mentions, ts, raw_json
) VALUES (
    ?1, ?2, ?3, ?4, ?5,
    ?6, ?7, ?8, ?9, ?10,
    ?11, ?12, ?13
);

-- name: ListReplayableWAMessages :many
-- Why: replay reads only messages not yet consumed (processed_at IS NULL), with a
-- captured payload (raw_json != ''), under the retry cap, past the write-settle grace
-- period, not currently held by another in-flight pop (or that hold gone stale), and
-- within the lookback window -- so a crashed scan does not resurrect ancient history.
SELECT message_id, chat_jid, raw_json, scan_attempts
FROM wa_messages
WHERE email = ?1
  AND processed_at IS NULL
  AND raw_json != ''
  AND scan_attempts < ?2
  AND created_at <= ?3
  AND (popped_at IS NULL OR popped_at <= ?4)
  AND ts >= ?5
ORDER BY ts
LIMIT ?6;

-- name: ListWAMessagesForChatSince :many
-- Why: reassess-wa-backlog needs raw_json (for ReplyToID matching) and a chat_name
-- filter that ListWAMessages does not expose (it filters chat_jid only).
SELECT message_id, sender, body, has_attachment, ts, raw_json
FROM wa_messages
WHERE email = ?1
  AND chat_name = ?2
  AND ts > ?3
ORDER BY ts;

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
