package store

import (
	"context"
	"message-consolidator/db"
	"sync"
	"time"
)

// settingsCacheTTL bounds staleness when an external write (e.g. another instance) mutates app_settings.
// Why: prefers prime intervals per project conventions; 23s is long enough to deflate libsql RTT but
// short enough that operator-driven config changes feel near-instant from a sibling instance.
const settingsCacheTTL = 23 * time.Second

var (
	settingsCacheMu  sync.RWMutex
	settingsCacheMap map[string]string
	settingsCachedAt time.Time
)

// GetSetting returns the DB-stored value for the given key, or fallback if absent or empty.
// Reads are served from an in-process cache (TTL settingsCacheTTL) that is invalidated on every
// UpsertSetting/DeleteSetting in this process.
func GetSetting(ctx context.Context, key, fallback string) string {
	v, ok := lookupCachedSetting(ctx, key)
	if !ok || v == "" {
		return fallback
	}
	return v
}

// GetSettingRaw returns the raw stored value (or empty string) without any fallback substitution.
// Useful for "is this key set?" checks where the absence of a row is meaningful.
func GetSettingRaw(ctx context.Context, key string) (string, bool) {
	v, ok := lookupCachedSetting(ctx, key)
	return v, ok
}

func lookupCachedSetting(ctx context.Context, key string) (string, bool) {
	settingsCacheMu.RLock()
	if settingsCacheMap != nil && time.Since(settingsCachedAt) < settingsCacheTTL {
		v, ok := settingsCacheMap[key]
		settingsCacheMu.RUnlock()
		return v, ok
	}
	settingsCacheMu.RUnlock()

	if err := refreshSettingsCache(ctx); err != nil {
		return "", false
	}
	settingsCacheMu.RLock()
	defer settingsCacheMu.RUnlock()
	v, ok := settingsCacheMap[key]
	return v, ok
}

// LoadAllSettings returns a snapshot of every persisted setting (key → value).
// Why: handlers need the full row set including updated_at/updated_by, so call ListAppSettings directly.
func LoadAllSettings(ctx context.Context) ([]db.AppSetting, error) {
	conn := GetDB()
	if conn == nil {
		return nil, nil
	}
	return db.New(conn).ListAppSettings(ctx)
}

// UpsertSetting persists a key/value pair and invalidates the in-process cache.
// Empty `value` is permitted; callers that want fallback behaviour should call DeleteSetting instead.
func UpsertSetting(ctx context.Context, key, value, updatedBy string) error {
	conn := GetDB()
	if err := db.New(conn).UpsertAppSetting(ctx, db.UpsertAppSettingParams{
		Key:       key,
		Value:     value,
		UpdatedBy: updatedBy,
	}); err != nil {
		return err
	}
	InvalidateSettingsCache()
	return nil
}

// DeleteSetting removes the row entirely so subsequent reads fall back to the .env / default value.
func DeleteSetting(ctx context.Context, key string) error {
	conn := GetDB()
	if err := db.New(conn).DeleteAppSetting(ctx, key); err != nil {
		return err
	}
	InvalidateSettingsCache()
	return nil
}

// InvalidateSettingsCache forces the next read to re-fetch from the database.
func InvalidateSettingsCache() {
	settingsCacheMu.Lock()
	settingsCacheMap = nil
	settingsCachedAt = time.Time{}
	settingsCacheMu.Unlock()
}

func refreshSettingsCache(ctx context.Context) error {
	conn := GetDB()
	if conn == nil {
		return nil
	}
	rows, err := db.New(conn).ListAppSettings(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Key] = r.Value
	}
	settingsCacheMu.Lock()
	settingsCacheMap = m
	settingsCachedAt = time.Now()
	settingsCacheMu.Unlock()
	return nil
}
