-- name: UpsertContactMapping :exec
INSERT INTO contacts (user_email, rep_name, aliases)
VALUES (?, ?, ?)
ON CONFLICT (user_email, rep_name)
DO UPDATE SET aliases = EXCLUDED.aliases;

-- name: DeleteContactMapping :exec
DELETE FROM contacts
WHERE user_email = ? AND rep_name = ?;
