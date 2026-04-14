-- name: LoadUsersAll :many
SELECT id, email, name, slack_id, wa_jid, picture, created_at 
FROM users;

-- name: LoadUserAliasesAll :many
SELECT user_id, alias_name FROM user_aliases;

-- name: LoadScanMetadataAll :many
SELECT user_email, source, target_id, last_ts FROM scan_metadata;

-- name: LoadGmailTokensAll :many
SELECT user_email, token_json FROM gmail_tokens;

-- name: LoadTenantAliasesAll :many
SELECT user_email, original_name, primary_name FROM tenant_aliases;

-- name: LoadContactsAllScan :many
SELECT tenant_email, canonical_id, display_name FROM contacts;

-- name: UpsertScanMetadata :exec
INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
VALUES (?, ?, ?, ?)
ON CONFLICT (user_email, source, target_id)
DO UPDATE SET last_ts = EXCLUDED.last_ts;

-- name: DeleteScanMetadataSlackThread :exec
DELETE FROM scan_metadata 
WHERE user_email = ? AND source = 'slack_thread' AND target_id = ?;
