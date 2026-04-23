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
		{"prompt_logs", queries.CreatePromptLogsTable},
		{"ai_inference_logs", queries.CreateAIInferenceLogsTable},
		{"contacts", queries.CreateContactsTable},
		{"identity_merge_history", queries.CreateIdentityMergeHistoryTable},
		{"contact_resolution", queries.CreateContactResolutionTable},
		{"identity_merge_candidates", queries.CreateIdentityMergeCandidatesTable},
		{"token_usage", queries.CreateTokenUsageTable},
	} {
		if err := step.fn(ctx); err != nil {
			return fmt.Errorf("failed to create %s table: %w", step.name, err)
		}
	}
	return nil
}

func runMigrations(ctx context.Context, q db.DBTX) error {
	migrateExistingData(ctx, q)
	go migrateContactResolution(ctx)
	return nil
}

// migrateContactResolution rebuilds contact_resolution on first run after the table was introduced.
func migrateContactResolution(ctx context.Context) {
	var count int
	_ = GetDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM contact_resolution").Scan(&count)
	if count > 0 {
		return
	}

	rows, err := GetDB().QueryContext(ctx, "SELECT DISTINCT tenant_email FROM contacts")
	if err != nil {
		return
	}
	defer rows.Close()

	var tenants []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err == nil {
			tenants = append(tenants, t)
		}
	}
	for _, t := range tenants {
		if err := RebuildContactResolution(ctx, t); err != nil {
			logger.Errorf("[RESOLUTION] rebuild failed for %s: %v", t, err)
		}
	}
	logger.Infof("[RESOLUTION] contact_resolution populated for %d tenants", len(tenants))
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
	// identity_merge_candidates holds only AI-generated proposals; data loss on schema change is acceptable.
	if !tableHasColumn(ctx, q, "identity_merge_candidates", "contact_id_a") {
		_, _ = q.ExecContext(ctx, "DROP TABLE IF EXISTS identity_merge_candidates")
		_ = db.New(q).CreateIdentityMergeCandidatesTable(ctx)
	}
	if !tableHasColumn(ctx, q, "identity_merge_history", "source_contact_id") {
		_, _ = q.ExecContext(ctx, "DROP TABLE IF EXISTS identity_merge_history")
		_ = db.New(q).CreateIdentityMergeHistoryTable(ctx)
	}

	_, _ = q.ExecContext(ctx, "ALTER TABLE identity_merge_candidates ADD COLUMN proposal_group_id TEXT")
	_, _ = q.ExecContext(ctx, "ALTER TABLE identity_merge_candidates ADD COLUMN canonical_name TEXT")

	_, _ = q.ExecContext(ctx, "UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET room = 'General' WHERE room IS NULL OR room = ''")
	_, _ = q.ExecContext(ctx, "UPDATE messages SET category = 'todo' WHERE category IN ('waiting', 'promise')")
}

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
	indexes := []string{
		// user_aliases
		"CREATE INDEX IF NOT EXISTS idx_user_aliases_user_id ON user_aliases(user_id)",
		// messages
		"CREATE INDEX IF NOT EXISTS idx_messages_dashboard_filter ON messages(user_email, is_deleted, done, category, assignee)",
		"CREATE INDEX IF NOT EXISTS idx_messages_thread_id ON messages(thread_id)",
		"CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task)",
		"CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room)",
		"CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages(requester)",
		"CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages(assignee)",
		"CREATE INDEX IF NOT EXISTS idx_messages_original_text ON messages(original_text)",
		"CREATE INDEX IF NOT EXISTS idx_messages_source ON messages(source)",
		"CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages(created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages(user_email)",
		"CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages(is_deleted)",
		"CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages(completed_at)",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_user_source_ts ON messages(user_email, source, source_ts)",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages(user_email, done, completed_at)",
		// contacts
		"CREATE INDEX IF NOT EXISTS idx_contacts_canonical ON contacts(canonical_id)",
		"CREATE INDEX IF NOT EXISTS idx_contacts_tenant_canonical ON contacts(tenant_email, canonical_id)",
		// slack_threads
		"CREATE INDEX IF NOT EXISTS idx_slack_threads_status ON slack_threads(status)",
	}
	for _, ddl := range indexes {
		_, _ = q.ExecContext(ctx, ddl)
	}
}
