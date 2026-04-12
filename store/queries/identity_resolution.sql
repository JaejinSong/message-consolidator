CREATE TABLE IF NOT EXISTS contact_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    identifier_type TEXT NOT NULL, -- 'email', 'slack_id', 'wa_jid', 'name'
    identifier_value TEXT NOT NULL,
    source TEXT NOT NULL,         -- 'slack', 'whatsapp', 'gmail', 'manual'
    trust_level INTEGER DEFAULT 1, -- 1: Low (Fuzzy), 5: High (Verified)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(contact_id, identifier_type, identifier_value)
);
CREATE INDEX IF NOT EXISTS idx_contact_aliases_lookup ON contact_aliases(identifier_value, identifier_type);

-- name: CreateIdentityMergeHistoryTable :exec
CREATE TABLE IF NOT EXISTS identity_merge_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_contact_id INTEGER NOT NULL,
    target_contact_id INTEGER NOT NULL,
    reason TEXT NOT NULL,
    merged_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateIdentityMergeCandidatesTable :exec
CREATE TABLE IF NOT EXISTS identity_merge_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id_a INTEGER NOT NULL,
    contact_id_b INTEGER NOT NULL,
    confidence REAL NOT NULL, -- 0.0 to 1.0
    reason TEXT,
    status TEXT DEFAULT 'pending', -- 'pending', 'approved', 'rejected'
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(contact_id_a, contact_id_b)
);

-- name: AddContactAlias :exec
INSERT OR IGNORE INTO contact_aliases (contact_id, identifier_type, identifier_value, source, trust_level)
VALUES (?, ?, ?, ?, ?);

-- name: GetContactAliases :many
SELECT id, contact_id, identifier_type, identifier_value, source, trust_level, created_at
FROM contact_aliases
WHERE contact_id = ?;

-- name: FindContactByAlias :one
SELECT contact_id
FROM contact_aliases
WHERE identifier_value = ? AND identifier_type = ?;

-- name: GetMergeCandidates :many
SELECT id, contact_id_a, contact_id_b, confidence, reason, status, created_at
FROM identity_merge_candidates
WHERE status = 'pending';

-- name: UpdateMergeCandidateStatus :exec
UPDATE identity_merge_candidates
SET status = ?
WHERE id = ?;

-- name: InsertMergeHistory :exec
INSERT INTO identity_merge_history (source_contact_id, target_contact_id, reason)
VALUES (?, ?, ?);

-- name: MigrateContactsRenameLegacyAliases :exec
ALTER TABLE contacts RENAME COLUMN aliases TO legacy_aliases_deprecated;

-- name: MigrateLegacyAliases :exec
INSERT OR IGNORE INTO contact_aliases (contact_id, identifier_type, identifier_value, source, trust_level)
SELECT contacts.id, 'name', trim(value), source, 1
FROM contacts, json_each('["' || replace(legacy_aliases_deprecated, ',', '","') || '"]')
WHERE legacy_aliases_deprecated != '' AND legacy_aliases_deprecated IS NOT NULL;

-- name: MigrateContactsDropLegacyAliases :exec
ALTER TABLE contacts DROP COLUMN legacy_aliases_deprecated;
