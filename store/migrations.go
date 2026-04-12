package store

import (
	"fmt"
	"os"
	"strings"
)

func createCoreTables(q Querier) error {
	// Why: Load the unified schema file first to ensure baseline tables and views exist.
	// This satisfies the "Unified Database" goal and ensures v_users is created.
	// We first try the relative path for app runtime, then the relative path for test execution.
	schemaPath := "store/queries/schema.sql"
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		schemaPath = "queries/schema.sql" // Try legacy/local root path
	}
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		schemaPath = "../store/queries/schema.sql" // Try relative path from test execution
	}

	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		// If we can't find the schema file, it's a fatal initialization error because views will be missing.
		return fmt.Errorf("failed to read schema.sql from %s: %w", schemaPath, err)
	}

	// Basic split and execute for the schema file
	queries := strings.Split(string(schema), ";")
	for _, query := range queries {
		trimmed := strings.TrimSpace(query)
		if trimmed != "" {
			if _, err := q.Exec(trimmed); err != nil {
				// Continue on error (e.g., table already exists) 
			}
		}
	}

	tables := []string{
		SQL.CreateUsersTable,
		SQL.CreateUserAliasesTable,
		SQL.CreateGmailTokensTable,
		SQL.CreateMessagesTable,
		SQL.CreateTaskTranslationsTable,
		SQL.CreateTenantAliasesTable,
		SQL.CreateScanMetadataTable,
		SQL.CreateSlackThreadsTable,
		SQL.CreateReportsTable,
		SQL.CreateReportTranslationsTable,
		SQL.CreateReportTranslationsIndex,
		SQL.CreatePromptLogsTable,
		SQL.CreateAIInferenceLogsTable,
		SQL.CreateAchievementsTable,
		SQL.CreateUserAchievementsTable,
		SQL.CreateContactsTable,
		SQL.CreateContactAliasesTable,
		SQL.CreateIdentityMergeHistoryTable,
		SQL.CreateIdentityMergeCandidatesTable,
	}
	for _, t := range tables {
		if t != "" {
			if _, err := q.Exec(t); err != nil {
				return fmt.Errorf("failed to create core table: %w", err)
			}
		}
	}
	return nil
}

func runMigrations(q Querier) error {
	//Why: Performs non-destructive column additions to the messages schema to support new features like categories and deadlines.
	migrations := []string{
		SQL.MigrateMessagesAddUserEmail,
		SQL.MigrateMessagesAddIsDeleted,
		SQL.MigrateMessagesAddRoom,
		SQL.MigrateMessagesAddDone,
		SQL.MigrateMessagesAddCompletedAt,
		SQL.MigrateMessagesAddOriginalText,
		SQL.MigrateMessagesAddCategory,
		SQL.MigrateMessagesAddDeadline,
		SQL.MigrateMessagesAddThreadID,
		SQL.MigrateMessagesAddAssigneeReason,
		SQL.MigrateMessagesAddRepliedToID,
		SQL.MigrateMessagesAddIsContextQuery,
		SQL.MigrateMessagesAddConstraints,
		SQL.MigrateMessagesAddMetadata,
		SQL.MigrateMessagesAddSourceChannels,
		SQL.MigrateMessagesAddConsolidatedContext,
		SQL.CreateIdxUserTS,
		SQL.MigrateUsersAddPoints,
		SQL.MigrateUsersAddStreak,
		SQL.MigrateUsersAddLevel,
		SQL.MigrateUsersAddXP,
		SQL.MigrateUsersAddDailyGoal,
		SQL.MigrateUsersAddLastCompletedAt,
		SQL.MigrateUsersAddStreakFreezes,
		SQL.MigrateAchievementsAddTargetValue,
		SQL.MigrateAchievementsAddXPReward,
		SQL.MigrateReportsAddIsTruncated,
		SQL.MigrateTaskTranslationsRenameLanguage,
		SQL.MigrateReportTranslationsRenameLanguage,
		SQL.MigrateTaskTranslationsAddLanguageCode,
		SQL.MigrateReportTranslationsAddLanguageCode,
		SQL.MigrateContactsAddContactType,
		SQL.MigrateContactsRenameLegacyAliases,
		SQL.MigrateLegacyAliases,
		SQL.MigrateContactsDropLegacyAliases,
	}

	for _, m := range migrations {
		if err := execMigration(q, m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	migrateExistingData(q)

	// Why: Views are essential for report generation and message resolution.
	// Moving them here ensures they are always recreated/updated on every startup.
	// Fixed: Rebuilding views with correct 'v_' prefix naming convention to match schema.sql.
	_, _ = q.Exec("DROP VIEW IF EXISTS v_contacts_resolved")
	if _, err := q.Exec(SQL.CreateContactsResolvedView); err != nil {
		if !strings.Contains(err.Error(), "locked") {
			return fmt.Errorf("failed to create v_contacts_resolved view: %w", err)
		}
	}

	_, _ = q.Exec("DROP VIEW IF EXISTS v_messages")
	if _, err := q.Exec(SQL.CreateMessagesView); err != nil {
		if !strings.Contains(err.Error(), "locked") {
			return fmt.Errorf("failed to create v_messages view: %w", err)
		}
	}

	_, _ = q.Exec("DROP VIEW IF EXISTS v_users")
	if _, err := q.Exec(SQL.CreateUsersView); err != nil {
		if !strings.Contains(err.Error(), "locked") {
			return fmt.Errorf("failed to create v_users view: %w", err)
		}
	}

	return nil
}

func execMigration(q Querier, query string) error {
	if query == "" {
		return nil
	}
	_, err := q.Exec(query)
	if err != nil {
		msg := strings.ToLower(err.Error())
		//Why: We ignore errors related to existing schema entities to allow idempotent migration runs.
		if strings.Contains(msg, "duplicate column") || 
		   strings.Contains(msg, "already exists") || 
		   strings.Contains(msg, "duplicate index") ||
		   strings.Contains(msg, "no such column") {
			return nil
		}
		return err
	}
	return nil
}

func migrateExistingData(q Querier) {
	//Why: Normalizes existing data by ensuring critical fields are populated with valid default values.
	_, _ = q.Exec(SQL.MigrateDataNormalizeIsDeleted)
	_, _ = q.Exec(SQL.MigrateDataNormalizeRoom)
	_, _ = q.Exec(SQL.MigrateDataNormalizeCategoryWaiting)
	_, _ = q.Exec(SQL.MigrateDataNormalizeCategoryPromise)
}

func createIndexes(q Querier) {
	indexes := []string{
		SQL.CreateIdxMessagesTask,
		SQL.CreateIdxMessagesRoom,
		SQL.CreateIdxMessagesRequester,
		SQL.CreateIdxMessagesAssignee,
		SQL.CreateIdxMessagesOriginalText,
		SQL.CreateIdxMessagesSource,
		SQL.CreateIdxMessagesCreatedAtDesc,
		SQL.CreateIdxMessagesUserEmail,
		SQL.CreateIdxMessagesIsDeleted,
		SQL.CreateIdxMessagesCompletedAt,
		SQL.CreateIdxMessagesUserSourceTS,
		SQL.CreateIdxTaskTranslationsIDLangCode,
		SQL.CreateIdxMessagesUserDoneCompleted,
	}
	for _, idx := range indexes {
		if err := execMigration(q, idx); err != nil {
			// Log but don't fail for index creation in this late stage.
			fmt.Printf("Warning: Failed to create index: %v\n", err)
		}
	}
}
