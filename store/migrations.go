package store

import (
	"context"
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
	if err := queries.CreateContactAliasesTable(ctx); err != nil {
		return fmt.Errorf("failed to create contact_aliases table: %w", err)
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
	// Why: Execute all required schema migrations using raw SQL.
	// We log errors for each step to ensure visibility if a migration fails during deployment.
	run := func(name string, query string) {
		if _, err := q.ExecContext(ctx, query); err != nil {
			logger.Warnf("[MIGRATION] %s failed (expected if already applied): %v", name, err)
		}
	}

	run("AddUserEmail", "ALTER TABLE messages ADD COLUMN user_email TEXT")
	run("AddIsDeleted", "ALTER TABLE messages ADD COLUMN is_deleted BOOLEAN DEFAULT 0")
	run("AddRoom", "ALTER TABLE messages ADD COLUMN room TEXT")
	run("AddDone", "ALTER TABLE messages ADD COLUMN done BOOLEAN DEFAULT 0")
	run("AddCompletedAt", "ALTER TABLE messages ADD COLUMN completed_at DATETIME")
	run("AddOriginalText", "ALTER TABLE messages ADD COLUMN original_text TEXT")
	run("AddCategory", "ALTER TABLE messages ADD COLUMN category TEXT DEFAULT 'todo'")
	run("AddDeadline", "ALTER TABLE messages ADD COLUMN deadline TEXT")
	run("AddThreadID", "ALTER TABLE messages ADD COLUMN thread_id TEXT")
	run("AddAssigneeReason", "ALTER TABLE messages ADD COLUMN assignee_reason TEXT")
	run("AddRepliedToID", "ALTER TABLE messages ADD COLUMN replied_to_id TEXT")
	run("AddIsContextQuery", "ALTER TABLE messages ADD COLUMN is_context_query INTEGER DEFAULT 0")
	run("AddPinned", "ALTER TABLE messages ADD COLUMN pinned BOOLEAN DEFAULT FALSE")
	run("AddConstraints", "ALTER TABLE messages ADD COLUMN constraints TEXT DEFAULT '[]'")
	run("AddMetadata", "ALTER TABLE messages ADD COLUMN metadata TEXT DEFAULT '{}'")
	run("AddSourceChannels", "ALTER TABLE messages ADD COLUMN source_channels TEXT DEFAULT '[]'")
	run("AddConsolidatedContext", "ALTER TABLE messages ADD COLUMN consolidated_context TEXT DEFAULT '[]'")
	run("AddSubtasks", "ALTER TABLE messages ADD COLUMN subtasks TEXT DEFAULT '[]'")

	_, _ = q.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_user_ts ON messages(user_email, source_ts)")

	run("ReportsAddIsTruncated", "ALTER TABLE reports ADD COLUMN is_truncated INTEGER DEFAULT 0")
	run("ReportsAddStatus", "ALTER TABLE reports ADD COLUMN status TEXT DEFAULT 'completed'")

	run("TaskRenameLang", "ALTER TABLE task_translations RENAME COLUMN language TO language_deprecated")
	run("ReportRenameLang", "ALTER TABLE report_translations RENAME COLUMN language TO language_deprecated")
	run("TaskAddLangCode", "ALTER TABLE task_translations ADD COLUMN language_code TEXT NOT NULL DEFAULT 'en'")
	run("ReportAddLangCode", "ALTER TABLE report_translations ADD COLUMN language_code TEXT NOT NULL DEFAULT 'en'")
	run("ContactsAddType", "ALTER TABLE contacts ADD COLUMN contact_type TEXT DEFAULT 'none'")
	run("LegacyAliases", "UPDATE contacts SET source = 'all' WHERE source IS NULL")
	run("TokenUsageAddFiltered", "ALTER TABLE token_usage ADD COLUMN filtered_count INTEGER DEFAULT 0")

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
	// Why: Basic data normalization for existing records.
	_, _ = q.ExecContext(ctx, "UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET room = 'General' WHERE room IS NULL OR room = ''")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET category = 'todo' WHERE category = 'waiting'")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET category = 'todo' WHERE category = 'promise'")
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
