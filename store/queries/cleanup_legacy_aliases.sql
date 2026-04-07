-- name: HardCleanupLegacyAliases :exec
-- Why: Permanently removes the legacy 'aliases' column after the Identity-X migration is verified.
-- Note: Requires SQLite 3.35.0+ for DROP COLUMN support.
ALTER TABLE contacts DROP COLUMN legacy_aliases_deprecated;
