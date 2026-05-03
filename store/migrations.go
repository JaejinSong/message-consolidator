package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"message-consolidator/db"
)

// schemaVersion gates DDL replay on startup. Bump whenever this file changes
// (new tables, view rebuild logic, indexes, FTS) so existing prod DBs re-run
// migrations on next deploy. Stored in app_settings under key "schema_version".
const schemaVersion = 5

func schemaIsCurrent(ctx context.Context, dbConn *sql.DB) bool {
	queries := db.New(dbConn)
	row, err := queries.GetAppSetting(ctx, "schema_version")
	if err != nil {
		return false
	}
	return row.Value == strconv.Itoa(schemaVersion)
}

func stampSchemaVersion(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	return queries.UpsertAppSetting(ctx, db.UpsertAppSettingParams{
		Key:       "schema_version",
		Value:     strconv.Itoa(schemaVersion),
		UpdatedBy: "system",
	})
}

func createCoreTables(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	for _, step := range []struct {
		name string
		fn   func(context.Context) error
	}{
		{"users", queries.CreateUsersTable},
		{"user_aliases", queries.CreateUserAliasesTable},
		{"gmail_tokens", queries.CreateGmailTokensTable},
		{"messages", queries.CreateMessagesTable},
		{"task_translations", queries.CreateTaskTranslationsTable},
		{"tenant_aliases", queries.CreateTenantAliasesTable},
		{"scan_metadata", queries.CreateScanMetadataTable},
		{"slack_threads", queries.CreateSlackThreadsTable},
		{"reports", queries.CreateReportsTable},
		{"report_translations", queries.CreateReportTranslationsTable},
		{"ai_inference_logs", queries.CreateAIInferenceLogsTable},
		{"contacts", queries.CreateContactsTable},
		{"identity_merge_history", queries.CreateIdentityMergeHistoryTable},
		{"contact_resolution", queries.CreateContactResolutionTable},
		{"identity_merge_candidates", queries.CreateIdentityMergeCandidatesTable},
		{"token_usage", queries.CreateTokenUsageTable},
		{"telegram_sessions", queries.CreateTelegramSessionsTable},
		{"telegram_credentials", queries.CreateTelegramCredentialsTable},
		{"app_settings", queries.CreateAppSettingsTable},
		{"message_embeddings", queries.CreateMessageEmbeddingsTable},
	} {
		if err := step.fn(ctx); err != nil {
			return fmt.Errorf("failed to create %s table: %w", step.name, err)
		}
	}
	if err := createMessagesFTS(ctx, q); err != nil {
		return fmt.Errorf("failed to create messages_fts: %w", err)
	}
	return nil
}

// createMessagesFTS provisions the fts5 virtual table over messages plus the three
// sync triggers. Why: kept out of sqlc because sqlc-sqlite truncates trigger bodies
// at the first ';' inside BEGIN…END, so each trigger would lose its tail statement.
// IF NOT EXISTS makes every step idempotent across cold starts and existing prod DBs.
func createMessagesFTS(ctx context.Context, q db.DBTX) error {
	stmts := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			task, original_text, requester, assignee,
			content='messages', content_rowid='id',
			tokenize='trigram case_sensitive 0'
		)`,
		`CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, task, original_text, requester, assignee)
			VALUES (new.id, new.task, new.original_text, new.requester, new.assignee);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, task, original_text, requester, assignee)
			VALUES ('delete', old.id, old.task, old.original_text, old.requester, old.assignee);
		END`,
		`CREATE TRIGGER IF NOT EXISTS messages_au AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, task, original_text, requester, assignee)
			VALUES ('delete', old.id, old.task, old.original_text, old.requester, old.assignee);
			INSERT INTO messages_fts(rowid, task, original_text, requester, assignee)
			VALUES (new.id, new.task, new.original_text, new.requester, new.assignee);
		END`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func rebuildViews(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	_, _ = q.ExecContext(ctx, "DROP VIEW IF EXISTS v_contacts_resolved")
	if err := queries.CreateContactsResolvedView(ctx); err != nil {
		return fmt.Errorf("failed to create v_contacts_resolved: %w", err)
	}
	_, _ = q.ExecContext(ctx, "DROP VIEW IF EXISTS v_messages")
	if err := queries.CreateMessagesView(ctx); err != nil {
		return fmt.Errorf("failed to create v_messages: %w", err)
	}
	return nil
}

func createIndexes(ctx context.Context, q db.DBTX) {
	indexes := []string{
		// user_aliases
		"CREATE INDEX IF NOT EXISTS idx_user_aliases_user_id ON user_aliases(user_id)",
		// messages
		"CREATE INDEX IF NOT EXISTS idx_messages_dashboard_filter ON messages(user_email, is_deleted, done, category, assignee)",
		"CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task)",
		"CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room)",
		"CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages(requester)",
		"CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages(assignee)",
		"CREATE INDEX IF NOT EXISTS idx_messages_source ON messages(source)",
		"CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages(is_deleted)",
		"CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages(completed_at)",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages(user_email, done, completed_at)",
		// contacts
		"CREATE INDEX IF NOT EXISTS idx_contacts_canonical ON contacts(canonical_id)",
		"CREATE INDEX IF NOT EXISTS idx_contacts_tenant_canonical ON contacts(tenant_email, canonical_id)",
		"CREATE INDEX IF NOT EXISTS idx_contacts_tenant_display_name ON contacts(tenant_email, LOWER(display_name))",
		// slack_threads
		"CREATE INDEX IF NOT EXISTS idx_slack_threads_status ON slack_threads(status)",
		// archive: is_archived narrows to the full set, done/is_deleted cover status filtering
		"CREATE INDEX IF NOT EXISTS idx_messages_archive_filter ON messages(user_email, is_archived, done, is_deleted)",
		// message_embeddings: SemanticTopK JOINs by model then message_id.
		"CREATE INDEX IF NOT EXISTS idx_msg_emb_model_id ON message_embeddings(model, message_id)",
	}
	for _, ddl := range indexes {
		_, _ = q.ExecContext(ctx, ddl)
	}
}

// addMessagesUpdatedAtColumn adds updated_at to messages on existing DBs.
// Why: SQLite does not support IF NOT EXISTS for ALTER TABLE, so we check pragma_table_info first.
// Safe to re-run — idempotent via the column existence check.
func addMessagesUpdatedAtColumn(ctx context.Context, q db.DBTX) error {
	var has int
	_ = q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name='updated_at'`,
	).Scan(&has)
	if has > 0 {
		return nil
	}
	if _, err := q.ExecContext(ctx,
		`ALTER TABLE messages ADD COLUMN updated_at DATETIME NOT NULL DEFAULT '1970-01-01T00:00:00Z'`,
	); err != nil {
		return fmt.Errorf("add updated_at column: %w", err)
	}
	// Why: ALTER TABLE DEFAULT fills existing rows with the ALTER execution time, not
	// created_at. Backfill so "no update" rows show their registration date correctly.
	if _, err := q.ExecContext(ctx,
		`UPDATE messages SET updated_at = created_at WHERE updated_at <= '1970-01-02' OR updated_at >= datetime('now', '-1 minute')`,
	); err != nil {
		return fmt.Errorf("backfill updated_at: %w", err)
	}
	return nil
}

// migrateEmbeddingsToF32 atomically recreates message_embeddings with vec typed
// as F32_BLOB(768) so libsql can run vector_distance_cos on the server side.
// Why: SQLite cannot ALTER a column's type; the only path is CREATE/INSERT/DROP/RENAME.
// The existing little-endian float32 bytes are copied 1:1 because F32_BLOB stores
// the same raw encoding that Float32sToBytes produces.
// Idempotent: skipped when the table already declares F32_BLOB.
func migrateEmbeddingsToF32(ctx context.Context, q db.DBTX) error {
	var count int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='message_embeddings'`,
	).Scan(&count); err != nil || count == 0 {
		return err
	}
	var ddl string
	_ = q.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_master WHERE name='message_embeddings'`,
	).Scan(&ddl)
	if strings.Contains(ddl, "F32_BLOB") {
		return nil
	}
	stmts := []string{
		`CREATE TABLE message_embeddings_new (
			message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
			model TEXT NOT NULL, dim INTEGER NOT NULL,
			vec F32_BLOB(768) NOT NULL, text_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO message_embeddings_new
			SELECT message_id, model, dim, vec, text_hash, created_at
			FROM message_embeddings WHERE dim = 768`,
		`DROP TABLE message_embeddings`,
		`ALTER TABLE message_embeddings_new RENAME TO message_embeddings`,
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate embeddings to F32_BLOB: %w", err)
		}
	}
	return nil
}
