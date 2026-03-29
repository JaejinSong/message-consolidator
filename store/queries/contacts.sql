-- name: UpsertContactMapping :exec
INSERT INTO contacts (tenant_email, canonical_id, display_name, aliases, source)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (tenant_email, canonical_id)
DO UPDATE SET 
    display_name = EXCLUDED.display_name,
    aliases = EXCLUDED.aliases,
    source = EXCLUDED.source;

-- name: DeleteContactMapping :exec
DELETE FROM contacts
WHERE tenant_email = ? AND canonical_id = ?;

-- name: LoadContactsAll :many
SELECT tenant_email, canonical_id, display_name, aliases, source FROM contacts;

-- name: GetContactByIdentifier :many
SELECT id, tenant_email, canonical_id, display_name, aliases, source 
FROM contacts 
WHERE tenant_email = ? 
AND (canonical_id = ? OR display_name = ? OR aliases LIKE '%' || ? || '%');

