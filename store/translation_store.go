package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

var (
	translationCache = make(map[string]map[int]string) //Why: Maps language codes and message IDs to translated text for fast lookups.
	translationMu    sync.RWMutex
)

func GetTaskTranslation(messageID int, langCode string) (string, error) {
	translationMu.RLock()
	if langCache, ok := translationCache[langCode]; ok {
		if text, exists := langCache[messageID]; exists {
			translationMu.RUnlock()
			return text, nil
		}
	}
	translationMu.RUnlock()

	var translatedText string
	err := db.QueryRow(SQL.GetTaskTranslation, messageID, langCode).Scan(&translatedText)
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

func GetTaskTranslationsBatch(messageIDs []int, langCode string) (map[int]string, error) {
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

	//Why: Uses an IN clause as a fallback because some drivers may not fully support ANY($1) for slice parameters.
	placeholders := make([]string, len(missingIDs))
	args := make([]interface{}, len(missingIDs)+1)
	args[0] = langCode
	for i, id := range missingIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	//Why: Hardcodes the query to prevent potential template conversion errors related to dynamic placeholder generation in external SQL files.
	query := fmt.Sprintf("SELECT message_id, translated_text FROM task_translations WHERE language_code = ? AND message_id IN (%s)", strings.Join(placeholders, ","))
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dbResults := make(map[int]string)
	for rows.Next() {
		var id int
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			continue
		}
		dbResults[id] = text
		results[id] = text
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

func SaveTaskTranslation(messageID int, langCode, translatedText string) error {
	_, err := db.Exec(SQL.UpsertTaskTranslation, messageID, langCode, translatedText)

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
func SaveTaskTranslationsBulk(langCode string, results map[int]string) error {
	tx, err := db.Begin()
	if err != nil { return err }
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES (?, ?, ?) ON CONFLICT(message_id, language_code) DO UPDATE SET translated_text=excluded.translated_text")
	if err != nil { return err }
	defer stmt.Close()

	for id, text := range results {
		if _, err := stmt.Exec(int(id), langCode, text); err != nil { return err }
	}

	if err := tx.Commit(); err == nil {
		syncCacheBatch(langCode, results)
	}
	return nil
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
