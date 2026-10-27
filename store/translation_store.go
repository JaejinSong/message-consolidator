package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"strings"
)

func GetTaskTranslationsBatch(ctx context.Context, messageIDs []MessageID, langCode string) (map[MessageID]string, error) {
	if langCode == "" {
		langCode = "en"
	}
	results := make(map[MessageID]string)
	if len(messageIDs) == 0 {
		return results, nil
	}
	nullIDs := make([]sql.NullInt64, len(messageIDs))
	for i, id := range messageIDs {
		nullIDs[i] = nullInt64(int64(id))
	}
	rows, err := db.New(GetDB()).GetTaskTranslationsBatch(ctx, db.GetTaskTranslationsBatchParams{
		LanguageCode: langCode,
		MessageIds:   nullIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("get task translations: %w", err)
	}
	for _, row := range rows {
		results[MessageID(row.MessageID.Int64)] = row.TranslatedText
	}
	return results, nil
}

// Why: A single batch write limits database lock contention.
func SaveTaskTranslationsBulk(ctx context.Context, langCode string, results map[MessageID]string) error {
	if langCode == "" {
		langCode = "en"
	}
	if len(results) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(results))
	// Why: ExecContext requires mixed ID, language and text values in its variadic arguments.
	args := make([]any, 0, len(results)*3)
	for id, text := range results {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, int64(id), langCode, text)
	}

	// Why: Generated placeholders keep all user values in bound arguments.
	query := fmt.Sprintf("INSERT INTO task_translations (message_id, language_code, translated_text) VALUES %s ON CONFLICT(message_id, language_code) DO UPDATE SET translated_text=excluded.translated_text", strings.Join(placeholders, ",")) //nolint:gosec // Placeholders are constant tokens, not interpolated user input.
	conn := GetDB()
	_, err := conn.ExecContext(ctx, query, args...)
	return err
}

// Why: Reusing the caller's transaction avoids deadlocks with a single connection.
func InvalidateTaskTranslation(ctx context.Context, q Querier, messageID MessageID) {
	_ = invalidateTaskTranslation(ctx, q, messageID)
}

func invalidateTaskTranslation(ctx context.Context, q Querier, messageID MessageID) error {
	if q == nil {
		q = GetDB()
	}
	if err := db.New(q).DeleteTaskTranslations(ctx, nullInt64(int64(messageID))); err != nil {
		return fmt.Errorf("delete task translations: %w", err)
	}
	return nil
}
