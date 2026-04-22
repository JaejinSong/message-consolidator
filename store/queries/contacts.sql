-- name: UpsertContactMapping :one
INSERT INTO contacts (tenant_email, canonical_id, display_name, source)
VALUES (?, ?, ?, ?)
ON CONFLICT(tenant_email, canonical_id) DO UPDATE SET
    display_name = IIF(EXCLUDED.display_name != '' AND EXCLUDED.display_name != contacts.canonical_id, EXCLUDED.display_name, contacts.display_name),
    source = EXCLUDED.source
RETURNING id;

-- name: DeleteContactMapping :exec
DELETE FROM contacts
WHERE tenant_email = ? AND canonical_id = ?;

-- name: LoadContactsAll :many
SELECT tenant_email, canonical_id, display_name, source, contact_type FROM contacts;

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

-- name: GetContactsByValues :many
SELECT id, canonical_id, display_name, secondary_ids FROM contacts
WHERE LOWER(canonical_id) IN (sqlc.slice('values'))
   OR LOWER(display_name) IN (sqlc.slice('values'))
   OR EXISTS (SELECT 1 FROM json_each(secondary_ids) WHERE LOWER(value) IN (sqlc.slice('values')));

-- name: GetContactsByTenant :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type, secondary_ids FROM contacts WHERE tenant_email = ?;

-- name: GetContactByID :one
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type, secondary_ids FROM contacts WHERE tenant_email = ? AND id = ?;

-- name: AppendSecondaryID :exec
UPDATE contacts
SET secondary_ids = json_insert(COALESCE(secondary_ids, '[]'), '$[#]', ?1)
WHERE id = ?2;


-- name: InsertMergeHistory :exec
INSERT INTO identity_merge_history (source_contact_id, target_contact_id, reason)
VALUES (?, ?, ?);

-- name: UpsertContactResolution :exec
INSERT INTO contact_resolution (tenant_email, raw_identifier, contact_id)
VALUES (?, ?, ?)
ON CONFLICT(tenant_email, raw_identifier) DO UPDATE SET contact_id = excluded.contact_id;

-- name: GetResolutionsByIdentifiers :many
SELECT raw_identifier, contact_id FROM contact_resolution
WHERE tenant_email = ? AND raw_identifier IN (sqlc.slice('identifiers'));

-- name: UpdateResolutionContactID :exec
UPDATE contact_resolution SET contact_id = ?1 WHERE tenant_email = ?2 AND contact_id = ?3;

-- name: GetContactsByIDs :many
SELECT id, tenant_email, canonical_id, display_name, source, master_contact_id, contact_type, secondary_ids
FROM contacts WHERE id IN (sqlc.slice('ids'));

-- name: GetContactTypeByID :one
SELECT contact_type FROM contacts WHERE id = ?1 AND tenant_email = ?2;

-- name: GetMasterAndTypeByID :one
SELECT master_contact_id, contact_type FROM contacts WHERE id = ?1 AND tenant_email = ?2;

-- name: FlattenContactChildren :exec
UPDATE contacts SET master_contact_id = ?1 WHERE master_contact_id = ?2 AND tenant_email = ?3;

-- name: GetDisplayNameByID :one
SELECT display_name FROM contacts WHERE id = ?;

-- name: UpdateDisplayNameIfEmpty :exec
UPDATE contacts SET display_name = ?1 WHERE id = ?2 AND tenant_email = ?3 AND (display_name IS NULL OR display_name = '');

-- name: GetTenantEmailByContactID :one
SELECT tenant_email FROM contacts WHERE id = ?;
