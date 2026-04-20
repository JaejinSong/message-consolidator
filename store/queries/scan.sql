-- name: LoadUsersAll :many
SELECT id, email, name, slack_id, wa_jid, picture, created_at 
FROM users;

-- name: LoadScanMetadataAll :many
SELECT user_email, source, target_id, last_ts FROM scan_metadata;

-- name: LoadGmailTokensAll :many
SELECT user_email, token_json FROM gmail_tokens;

-- name: UpsertScanMetadata :exec
INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_email, source, target_id)
DO UPDATE SET last_ts = EXCLUDED.last_ts;

-- name: DeleteScanMetadataSlackThread :exec
DELETE FROM scan_metadata 
WHERE user_email = ? AND source = 'slack_thread' AND target_id = ?;

-- name: IsSourceTSProcessed :one
SELECT EXISTS(
    SELECT 1 FROM scan_metadata 
    WHERE user_email = ? AND source = 'processed_msg' AND target_id = ?
);

-- name: MarkSourceTSProcessed :exec
INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
VALUES (?, 'processed_msg', ?, datetime('now'))
ON CONFLICT (user_email, source, target_id) DO NOTHING;
