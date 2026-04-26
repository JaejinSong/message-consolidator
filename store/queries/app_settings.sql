-- name: GetAppSetting :one
SELECT key, value, updated_at, updated_by FROM app_settings WHERE key = ?1;

-- name: ListAppSettings :many
SELECT key, value, updated_at, updated_by FROM app_settings;

-- name: UpsertAppSetting :exec
INSERT INTO app_settings (key, value, updated_by, updated_at)
VALUES (?1, ?2, ?3, CURRENT_TIMESTAMP)
ON CONFLICT(key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_by = EXCLUDED.updated_by,
    updated_at = CURRENT_TIMESTAMP;

-- name: DeleteAppSetting :exec
DELETE FROM app_settings WHERE key = ?1;
