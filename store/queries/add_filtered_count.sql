-- name: MigrateTokenUsageAddFilteredCount :exec
ALTER TABLE token_usage ADD COLUMN filtered_count INTEGER DEFAULT 0;
