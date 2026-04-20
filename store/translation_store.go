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

func GetTaskTranslation(ctx context.Context, messageID int, langCode string) (string, error) {
	if langCode == "" { langCode = "en" }
	translationMu.RLock()
	if langCache, ok := translationCache[langCode]; ok {
		if text, exists := langCache[messageID]; exists {
			translationMu.RUnlock()
			return text, nil
		}
	}
	translationMu.RUnlock()

	conn := GetDB()
	queries := db.New(conn)
	translatedText, err := queries.GetTaskTranslation(ctx, db.GetTaskTranslationParams{
		MessageID:    nullInt64(int64(messageID)),
		LanguageCode: langCode,
	})
	if err == sql.ErrNoRows {
		translationMu.Lock()
		if translationCache[langCode] == nil {
			translationCache[langCode] = make(map[int]string)
		}
		translationCache[langCode][messageID] = "" //Why: Caches the absence of a translation to prevent redundant database queries for non-existent records.
		translationMu.Unlock()
		return "", nil
	}

	if err == nil && translatedText != "" {
		translationMu.Lock()
		if translationCache[langCode] == nil {
			translationCache[langCode] = make(map[int]string)
		}
		translationCache[langCode][messageID] = translatedText
		translationMu.Unlock()
	}

	return translatedText, err
}

func GetTaskTranslationsBatch(ctx context.Context, messageIDs []int, langCode string) (map[int]string, error) {
	if langCode == "" { langCode = "en" }
	if len(messageIDs) == 0 {
		return make(map[int]string), nil
	}

	results := make(map[int]string)
	var missingIDs []int

	translationMu.RLock()
	langCache, ok := translationCache[langCode]
	if ok {
		for _, id := range messageIDs {
			if text, exists := langCache[id]; exists {
				if text != "" {
					results[id] = text
				}
			} else {
				missingIDs = append(missingIDs, id)
			}
		}
	} else {
		missingIDs = append(missingIDs, messageIDs...) //Why: Forces a full database lookup if the requested language is completely missing from the cache.
	}
	translationMu.RUnlock()

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

func SaveTaskTranslation(ctx context.Context, messageID int, langCode, translatedText string) error {
	if langCode == "" { langCode = "en" }
	conn := GetDB()
	queries := db.New(conn)
	err := queries.UpsertTaskTranslation(ctx, db.UpsertTaskTranslationParams{
		MessageID:      nullInt64(int64(messageID)),
		LanguageCode:   langCode,
		TranslatedText: translatedText,
	})

	if err == nil {
		translationMu.Lock()
		if translationCache[langCode] == nil {
			translationCache[langCode] = make(map[int]string)
		}
		translationCache[langCode][messageID] = translatedText
		translationMu.Unlock()
	}

	return err
}

// SaveTaskTranslationsBulk saves multiple translations in a single transaction.
// Why: Minimizes database lock contention and ensures atomicity for batch AI results.
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

	query := fmt.Sprintf("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES %s ON CONFLICT(message_id, language_code) DO UPDATE SET translated_text=excluded.translated_text", strings.Join(placeholders, ","))
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
