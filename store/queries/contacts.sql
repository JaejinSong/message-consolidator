-- name: UpsertContactMapping :one
INSERT INTO contacts (tenant_email, canonical_id, display_name, source)
VALUES (?, ?, ?, ?)
ON CONFLICT(tenant_email, canonical_id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    source = EXCLUDED.source
RETURNING id;

-- name: DeleteContactMapping :exec
DELETE FROM contacts
WHERE tenant_email = ? AND canonical_id = ?;

-- name: LoadContactsAll :many
SELECT tenant_email, canonical_id, display_name, source, contact_type FROM contacts;

-- name: GetContactByIdentifier :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type
FROM contacts 
WHERE tenant_email = ? 
AND (canonical_id = ? OR display_name = ?);

-- name: SearchContacts :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type
FROM contacts
WHERE tenant_email = ?
AND (display_name LIKE '%' || ? || '%' 
     OR canonical_id LIKE '%' || ? || '%')
LIMIT 20;

-- name: UpdateContactDetails :exec
UPDATE contacts
SET
    display_name = COALESCE(sqlc.narg('display_name'), display_name),
    source = COALESCE(sqlc.narg('source'), source),
    master_contact_id = sqlc.narg('master_contact_id'),
    contact_type = COALESCE(sqlc.narg('contact_type'), contact_type)
WHERE tenant_email = ?1 AND id = ?2;

-- name: GetLinkedContacts :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type
FROM contacts
WHERE tenant_email = ? AND master_contact_id IS NOT NULL;


-- name: GetContactsWithMaster :many
SELECT id, master_contact_id FROM contacts WHERE master_contact_id IS NOT NULL;

-- name: GetAliasesByValues :many
SELECT identifier_value, contact_id FROM contact_aliases WHERE identifier_value IN (sqlc.slice('values'));

-- name: GetContactsByTenant :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ?;

-- name: GetContactByID :one
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ? AND id = ?;


-- name: InsertMergeHistory :exec
INSERT INTO identity_merge_history (source_contact_id, target_contact_id, reason)
VALUES (?, ?, ?);
