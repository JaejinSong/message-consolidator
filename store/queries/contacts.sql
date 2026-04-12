-- name: UpsertContactMappingSimple :one
INSERT INTO contacts (tenant_email, canonical_id, display_name, source)
VALUES (?, ?, ?, ?)
ON CONFLICT(tenant_email, canonical_id) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    source = EXCLUDED.source
RETURNING id;

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
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type
FROM contacts
WHERE tenant_email = ? AND master_contact_id IS NOT NULL;

-- name: UpdateContactType :exec
UPDATE contacts
SET contact_type = ?
WHERE id = ?;

-- name: GetContactsWithMaster :many
SELECT id, master_contact_id FROM contacts WHERE master_contact_id IS NOT NULL;

-- name: GetAliasesByValues :many
SELECT identifier_value, contact_id FROM contact_aliases WHERE identifier_value IN (sqlc.slice('values'));

-- name: GetContactsByTenant :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ?;

-- name: GetContactByID :one
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type FROM contacts WHERE tenant_email = ? AND id = ?;

-- name: MigrateContactsAddContactType :exec
ALTER TABLE contacts ADD COLUMN contact_type TEXT DEFAULT 'none';

-- name: MigrateLegacyAliases :exec
INSERT OR IGNORE INTO contact_aliases (contact_id, identifier_type, identifier_value, source, trust_level)
SELECT id, 'legacy', canonical_id, source, 100 FROM contacts;

-- name: InsertMergeHistory :exec
INSERT INTO identity_merge_history (source_contact_id, target_contact_id, reason)
VALUES (?, ?, ?);
