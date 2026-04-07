-- name: MigrateContactsAddIsInternal :exec
ALTER TABLE contacts ADD COLUMN is_internal BOOLEAN DEFAULT 0;
