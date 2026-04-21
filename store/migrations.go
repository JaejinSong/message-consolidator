package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/db"
	"message-consolidator/logger"
)

func createCoreTables(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	// Why: Systematically create all required tables using sqlc methods.
	logger.Infof("[DB-INIT] Creating users table...")
	if err := queries.CreateUsersTable(ctx); err != nil {
		logger.Errorf("[DB-INIT] Failed to create users table: %v", err)
		return fmt.Errorf("failed to create users table: %w", err)
	}
	logger.Infof("[DB-INIT] Users table verified.")
	if err := queries.CreateUserAliasesTable(ctx); err != nil {
		return fmt.Errorf("failed to create user_aliases table: %w", err)
	}
	if err := queries.CreateGmailTokensTable(ctx); err != nil {
		return fmt.Errorf("failed to create gmail_tokens table: %w", err)
	}
	if err := queries.CreateMessagesTable(ctx); err != nil {
		logger.Errorf("[DB-INIT] Failed to create messages table: %v", err)
		return fmt.Errorf("failed to create messages table: %w", err)
	}
	_ = queries.CreateTaskTranslationsTable(ctx)
	_ = queries.CreateTenantAliasesTable(ctx)
	_ = queries.CreateScanMetadataTable(ctx)
	_ = queries.CreateSlackThreadsTable(ctx)
	_ = queries.CreateReportsTable(ctx)
	_ = queries.CreateReportTranslationsTable(ctx)
	_ = queries.CreatePromptLogsTable(ctx)
	if err := queries.CreateAIInferenceLogsTable(ctx); err != nil {
		return fmt.Errorf("failed to create ai_inference_logs table: %w", err)
	}

	if err := queries.CreateContactsTable(ctx); err != nil {
		logger.Errorf("[DB-INIT] Failed to create contacts table: %v", err)
		return fmt.Errorf("failed to create contacts table: %w", err)
	}
	if err := queries.CreateIdentityMergeHistoryTable(ctx); err != nil {
		return fmt.Errorf("failed to create identity_merge_history table: %w", err)
	}
	if err := queries.CreateIdentityMergeCandidatesTable(ctx); err != nil {
		return fmt.Errorf("failed to create identity_merge_candidates table: %w", err)
	}
	if err := queries.CreateTokenUsageTable(ctx); err != nil {
		return fmt.Errorf("failed to create token_usage table: %w", err)
	}
	return nil
}

func runMigrations(ctx context.Context, q db.DBTX) error {
	// All schema columns are defined in the DDL (CreateXxxTable).
	// Only data-level normalizations run here for existing deployments.
	migrateExistingData(ctx, q)
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

func migrateExistingData(ctx context.Context, q db.DBTX) {
	// If the table is missing core columns (e.g. leftover from an old schema), drop and recreate it.
	// identity_merge_candidates holds only AI-generated proposals so data loss is acceptable.
	if !tableHasColumn(ctx, q, "identity_merge_candidates", "contact_id_a") {
		_, _ = q.ExecContext(ctx, "DROP TABLE IF EXISTS identity_merge_candidates")
		_ = db.New(q).CreateIdentityMergeCandidatesTable(ctx)
	}

	// Why: ALTER TABLE ADD COLUMN is idempotent on SQLite when columns are added conditionally via ignored errors.
	_, _ = q.ExecContext(ctx, "ALTER TABLE identity_merge_candidates ADD COLUMN proposal_group_id TEXT")
	_, _ = q.ExecContext(ctx, "ALTER TABLE identity_merge_candidates ADD COLUMN canonical_name TEXT")

	// Why: Basic data normalization for existing records.
	_, _ = q.ExecContext(ctx, "UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET room = 'General' WHERE room IS NULL OR room = ''")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET category = 'todo' WHERE category = 'waiting'")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET category = 'todo' WHERE category = 'promise'")
}

// tableHasColumn reports whether the given SQLite table contains a column with the given name.
func tableHasColumn(ctx context.Context, q db.DBTX, table, column string) bool {
	rows, err := q.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

func createIndexes(ctx context.Context, q db.DBTX) {
	// Why: Create required indexes using raw SQL as sqlc didn't generate explicit methods for these IF NOT EXISTS statements.
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages(requester)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages(assignee)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_original_text ON messages(original_text)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_source ON messages(source)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages(created_at DESC)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages(user_email)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages(is_deleted)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages(completed_at)")
	_, _ = q.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_user_source_ts ON messages(user_email, source, source_ts)")
	_, _ = q.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages(user_email, done, completed_at)")
}
