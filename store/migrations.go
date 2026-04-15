package store

import (
	"context"
	"message-consolidator/db"
	"message-consolidator/logger"
)

func createCoreTables(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	// Why: Systematically create all required tables using sqlc methods.
	_ = queries.CreateUsersTable(ctx)
	_ = queries.CreateUserAliasesTable(ctx)
	_ = queries.CreateGmailTokensTable(ctx)
	_ = queries.CreateMessagesTable(ctx)
	_ = queries.CreateTaskTranslationsTable(ctx)
	_ = queries.CreateTenantAliasesTable(ctx)
	_ = queries.CreateScanMetadataTable(ctx)
	_ = queries.CreateSlackThreadsTable(ctx)
	_ = queries.CreateReportsTable(ctx)
	_ = queries.CreateReportTranslationsTable(ctx)
	_ = queries.CreatePromptLogsTable(ctx)
	_ = queries.CreateAIInferenceLogsTable(ctx)

	_ = queries.CreateContactsTable(ctx)
	_ = queries.CreateContactAliasesTable(ctx)
	_ = queries.CreateIdentityMergeHistoryTable(ctx)
	_ = queries.CreateIdentityMergeCandidatesTable(ctx)
	_ = queries.CreateTokenUsageTable(ctx)
	return nil
}

func runMigrations(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	// Why: Execute all required schema migrations using sqlc-generated methods.
	// We log errors for each step to ensure visibility if a migration fails during deployment.
	run := func(name string, f func(context.Context) error) {
		if err := f(ctx); err != nil {
			logger.Warnf("[MIGRATION] %s failed (expected if already applied): %v", name, err)
		}
	}

	run("AddUserEmail", queries.MigrateMessagesAddUserEmail)
	run("AddIsDeleted", queries.MigrateMessagesAddIsDeleted)
	run("AddRoom", queries.MigrateMessagesAddRoom)
	run("AddDone", queries.MigrateMessagesAddDone)
	run("AddCompletedAt", queries.MigrateMessagesAddCompletedAt)
	run("AddOriginalText", queries.MigrateMessagesAddOriginalText)
	run("AddCategory", queries.MigrateMessagesAddCategory)
	run("AddDeadline", queries.MigrateMessagesAddDeadline)
	run("AddThreadID", queries.MigrateMessagesAddThreadID)
	run("AddAssigneeReason", queries.MigrateMessagesAddAssigneeReason)
	run("AddRepliedToID", queries.MigrateMessagesAddRepliedToID)
	run("AddIsContextQuery", queries.MigrateMessagesAddIsContextQuery)
	run("AddPinned", queries.MigrateMessagesAddPinned)
	run("AddConstraints", queries.MigrateMessagesAddConstraints)
	run("AddMetadata", queries.MigrateMessagesAddMetadata)
	run("AddSourceChannels", queries.MigrateMessagesAddSourceChannels)
	run("AddConsolidatedContext", queries.MigrateMessagesAddConsolidatedContext)
	run("AddSubtasks", queries.MigrateMessagesAddSubtasks)

	_, _ = q.ExecContext(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS idx_user_ts ON messages(user_email, source_ts)")

	run("ReportsAddIsTruncated", queries.MigrateReportsAddIsTruncated)
	run("ReportsAddStatus", queries.MigrateReportsAddStatus)

	run("TaskRenameLang", queries.MigrateTaskTranslationsRenameLanguage)
	run("ReportRenameLang", queries.MigrateReportTranslationsRenameLanguage)
	run("TaskAddLangCode", queries.MigrateTaskTranslationsAddLanguageCode)
	run("ReportAddLangCode", queries.MigrateReportTranslationsAddLanguageCode)
	run("ContactsAddType", queries.MigrateContactsAddContactType)
	run("LegacyAliases", queries.MigrateLegacyAliases)
	run("TokenUsageAddFiltered", queries.MigrateTokenUsageAddFilteredCount)

	migrateExistingData(ctx, q)
	_ = rebuildViews(ctx, q)
	return nil
}

func rebuildViews(ctx context.Context, q db.DBTX) error {
	queries := db.New(q)
	_, _ = q.ExecContext(ctx, "DROP VIEW IF EXISTS v_contacts_resolved")
	_ = queries.CreateContactsResolvedView(ctx)
	_, _ = q.ExecContext(ctx, "DROP VIEW IF EXISTS v_messages")
	_ = queries.CreateMessagesView(ctx)

	return nil
}

func migrateExistingData(ctx context.Context, q db.DBTX) {
	queries := db.New(q)
	_ = queries.MigrateDataNormalizeIsDeleted(ctx)
	_ = queries.MigrateDataNormalizeRoom(ctx)
	_ = queries.MigrateDataNormalizeCategoryWaiting(ctx)
	_ = queries.MigrateDataNormalizeCategoryPromise(ctx)
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
