package store

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
)

var (
	translationCache = make(map[string]map[int]string) // language -> message_id -> translated_text
	translationMu    sync.RWMutex
)

func GetTaskTranslation(messageID int, language string) (string, error) {
	translationMu.RLock()
	if langCache, ok := translationCache[language]; ok {
		if text, exists := langCache[messageID]; exists {
			translationMu.RUnlock()
			return text, nil
		}
	}
	translationMu.RUnlock()

	var translatedText string
	err := db.QueryRow(SQL.GetTaskTranslation, messageID, language).Scan(&translatedText)
	if err == sql.ErrNoRows {
		translationMu.Lock()
		if translationCache[language] == nil {
			translationCache[language] = make(map[int]string)
		}
		translationCache[language][messageID] = "" // 번역 없음 상태도 캐싱
		translationMu.Unlock()
		return "", nil
	}

	if err == nil && translatedText != "" {
		translationMu.Lock()
		if translationCache[language] == nil {
			translationCache[language] = make(map[int]string)
		}
		translationCache[language][messageID] = translatedText
		translationMu.Unlock()
	}

	return translatedText, err
}

func GetTaskTranslationsBatch(messageIDs []int, language string) (map[int]string, error) {
	if len(messageIDs) == 0 {
		return make(map[int]string), nil
	}

	results := make(map[int]string)
	var missingIDs []int

	translationMu.RLock()
	langCache, ok := translationCache[language]
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
		missingIDs = append(missingIDs, messageIDs...) // 캐시에 해당 언어가 아예 없으면 전부 조회
	}
	translationMu.RUnlock()

	// 모든 번역이 캐시에 존재한다면 DB 조회 없이 즉시 반환
	if len(missingIDs) == 0 {
		return results, nil
	}

	// pgx/v5 stdlib가 []int64 타입의 ANY($1)를 지원하지 않는 경우를 위해 IN 처리
	placeholders := make([]string, len(missingIDs))
	args := make([]interface{}, len(missingIDs)+1)
	args[0] = language
	for i, id := range missingIDs {
		placeholders[i] = "?"
		args[i+1] = id
	}

	// 외부 SQL 파일의 템플릿 변환 오류를 방지하기 위해 명시적으로 하드코딩된 쿼리 사용
	query := fmt.Sprintf("SELECT message_id, translated_text FROM task_translations WHERE language = ? AND message_id IN (%s)", strings.Join(placeholders, ","))
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
	if translationCache[language] == nil {
		translationCache[language] = make(map[int]string)
	}
	for _, id := range missingIDs {
		if text, ok := dbResults[id]; ok {
			translationCache[language][id] = text
		} else {
			translationCache[language][id] = "" // DB에 없는 ID도 빈 값으로 캐싱
		}
	}
	translationMu.Unlock()

	return results, nil
}

func SaveTaskTranslation(messageID int, language, translatedText string) error {
	_, err := db.Exec(SQL.UpsertTaskTranslation, messageID, language, translatedText)

	if err == nil {
		translationMu.Lock()
		if translationCache[language] == nil {
			translationCache[language] = make(map[int]string)
		}
		translationCache[language][messageID] = translatedText
		translationMu.Unlock()
	}

	return err
}
