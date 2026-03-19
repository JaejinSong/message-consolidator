package store

import (
	"database/sql"
	"fmt"
	"strings"
)

func GetTaskTranslation(messageID int, language string) (string, error) {
	var translatedText string
	err := db.QueryRow("SELECT translated_text FROM task_translations WHERE message_id = $1 AND language = $2", messageID, language).Scan(&translatedText)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return translatedText, err
}

func GetTaskTranslationsBatch(messageIDs []int, language string) (map[int]string, error) {
	if len(messageIDs) == 0 {
		return make(map[int]string), nil
	}

	placeholders := make([]string, len(messageIDs))
	args := make([]interface{}, len(messageIDs)+1)
	args[0] = language
	for i, id := range messageIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}

	query := fmt.Sprintf("SELECT message_id, translated_text FROM task_translations WHERE language = $1 AND message_id IN (%s)", strings.Join(placeholders, ","))
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make(map[int]string)
	for rows.Next() {
		var id int
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			continue
		}
		results[id] = text
	}
	return results, nil
}

func SaveTaskTranslation(messageID int, language, translatedText string) error {
	_, err := db.Exec(`
		INSERT INTO task_translations (message_id, language, translated_text)
		VALUES ($1, $2, $3)
		ON CONFLICT (message_id, language) DO UPDATE SET translated_text = EXCLUDED.translated_text`,
		messageID, language, translatedText)
	return err
}
