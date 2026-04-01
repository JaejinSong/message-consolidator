-- name: AddContactMapping :exec
INSERT INTO contacts (tenant_email, canonical_id, display_name, aliases, source)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: UpsertContactMapping :exec
INSERT INTO contacts (tenant_email, canonical_id, display_name, aliases, source)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(tenant_email, canonical_id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    aliases = EXCLUDED.aliases,
    source = EXCLUDED.source
RETURNING id;

-- name: DeleteContactMapping :exec
DELETE FROM contacts
WHERE tenant_email = ? AND canonical_id = ?;

-- name: LoadContactsAll :many
SELECT tenant_email, canonical_id, display_name, aliases, source FROM contacts;

-- name: GetContactByIdentifier :many
SELECT id, tenant_email, canonical_id, display_name, aliases, source, master_contact_id
FROM contacts 
WHERE tenant_email = ? 
AND (canonical_id = ? OR display_name = ? OR aliases LIKE '%' || ? || '%');

-- name: SearchContacts :many
SELECT id, tenant_email, canonical_id, display_name, aliases, source, master_contact_id
FROM contacts
WHERE tenant_email = ?
AND (display_name LIKE '%' || ? || '%' 
     OR canonical_id LIKE '%' || ? || '%' 
     OR aliases LIKE '%' || ? || '%')
LIMIT 20;

-- name: UpdateContactLink :exec
UPDATE contacts
SET master_contact_id = ?
WHERE tenant_email = ? AND id = ?;

-- name: UnlinkContact :exec
UPDATE contacts
SET master_contact_id = NULL
WHERE tenant_email = ? AND id = ?;

-- name: FlattenChildren :exec
UPDATE contacts
SET master_contact_id = ?
WHERE tenant_email = ? AND master_contact_id = ?;

-- name: GetLinkedContacts :many
SELECT id, tenant_email, canonical_id, display_name, aliases, source, master_contact_id
FROM contacts
WHERE tenant_email = ? AND master_contact_id IS NOT NULL;
