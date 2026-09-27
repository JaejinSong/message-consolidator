-- name: UpsertSlackUserToken :exec
INSERT INTO slack_user_tokens (user_email, token_enc, slack_user_id, scopes, updated_at)
VALUES (?, ?, ?, ?, DATETIME('now'))
ON CONFLICT (user_email)
DO UPDATE SET token_enc = EXCLUDED.token_enc, slack_user_id = EXCLUDED.slack_user_id,
    scopes = EXCLUDED.scopes, updated_at = DATETIME('now');

-- name: GetSlackUserToken :one
SELECT token_enc, slack_user_id, scopes FROM slack_user_tokens WHERE user_email = ?;

-- name: DeleteSlackUserToken :exec
DELETE FROM slack_user_tokens WHERE user_email = ?;

-- name: LoadSlackUserTokensAll :many
SELECT user_email, token_enc, slack_user_id, scopes FROM slack_user_tokens;
