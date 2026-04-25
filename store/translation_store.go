package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"strings"
	"sync"
)

var (
	translationCache = make(map[string]map[int]string) //Why: Maps language codes and message IDs to translated text for fast lookups.
	translationMu    sync.RWMutex
)

func GetTaskTranslationsBatch(ctx context.Context, messageIDs []int, langCode string) (map[int]string, error) {
	if langCode == "" { langCode = "en" }
	if len(messageIDs) == 0 {
		return make(map[int]string), nil
	}

	results, missingIDs := splitTranslationsByCache(langCode, messageIDs)

	//Why: Skips the database query entirely if all requested translations are already available in the cache.
	if len(missingIDs) == 0 {
		return results, nil
	}

	conn := GetDB()
	queries := db.New(conn)
	nullIDs := make([]sql.NullInt64, len(missingIDs))
	for i, id := range missingIDs {
		nullIDs[i] = nullInt64(int64(id))
	}
	rows, err := queries.GetTaskTranslationsBatch(ctx, db.GetTaskTranslationsBatchParams{
		LanguageCode: langCode,
		MessageIds:   nullIDs,
	})
	if err != nil {
		return nil, err
	}

	dbResults := make(map[int]string)
	for _, row := range rows {
		mid := int(row.MessageID.Int64)
		dbResults[mid] = row.TranslatedText
		results[mid] = row.TranslatedText
	}

	translationMu.Lock()
	if translationCache[langCode] == nil {
		translationCache[langCode] = make(map[int]string)
	}
	for _, id := range missingIDs {
		if text, ok := dbResults[id]; ok {
			translationCache[langCode][id] = text
		} else {
			translationCache[langCode][id] = "" //Why: Caches message IDs that are missing from the database with empty strings to optimize future lookups.
		}
	}
	translationMu.Unlock()

	return results, nil
}

//Why: Cache hit returns the translated text immediately; misses (or absent language) bubble up to the SQL fetch.
func splitTranslationsByCache(langCode string, messageIDs []int) (map[int]string, []int) {
	results := make(map[int]string)
	translationMu.RLock()
	defer translationMu.RUnlock()
	langCache, ok := translationCache[langCode]
	if !ok {
		// Forces a full database lookup if the requested language is missing from the cache.
		missing := make([]int, len(messageIDs))
		copy(missing, messageIDs)
		return results, missing
	}
	var missing []int
	for _, id := range messageIDs {
		text, exists := langCache[id]
		if !exists {
			missing = append(missing, id)
			continue
		}
		if text != "" {
			results[id] = text
		}
	}
	return results, missing
}

// SaveTaskTranslationsBulk saves multiple translations in a single optimized SQL execution.
// Why: Minimizes database lock contention and ensures atomicity for batch AI results.
func SaveTaskTranslationsBulk(ctx context.Context, langCode string, results map[int]string) error {
	if langCode == "" { langCode = "en" }
	if len(results) == 0 { return nil }

	placeholders := make([]string, 0, len(results))
	args := make([]interface{}, 0, len(results)*3)
	for id, text := range results {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, id, langCode, text)
	}

	// Why: Placeholders are static "(?, ?, ?)" tokens generated from len(results); user data
	// flows through args, not the format string. SQL injection is structurally impossible here.
	query := fmt.Sprintf("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES %s ON CONFLICT(message_id, language_code) DO UPDATE SET translated_text=excluded.translated_text", strings.Join(placeholders, ",")) //nolint:gosec // Placeholders are constant tokens, not interpolated user input.
	conn := GetDB()
	_, err := conn.ExecContext(ctx, query, args...)
	if err == nil {
		syncCacheBatch(langCode, results)
	}
	return err
}

func syncCacheBatch(langCode string, results map[int]string) {
	translationMu.Lock()
	defer translationMu.Unlock()
	if translationCache[langCode] == nil {
		translationCache[langCode] = make(map[int]string)
	}
	for id, text := range results {
		translationCache[langCode][id] = text
	}
}

// InvalidateTaskTranslation removes a task's translation from both the in-memory cache
// and the DB so it will be re-translated JIT on the next dashboard load.
func InvalidateTaskTranslation(ctx context.Context, messageID int) {
	translationMu.Lock()
	for _, langCache := range translationCache {
		delete(langCache, messageID)
	}
	translationMu.Unlock()
	_ = db.New(GetDB()).DeleteTaskTranslations(ctx, nullInt64(int64(messageID)))
}
