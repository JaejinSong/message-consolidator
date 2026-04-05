package store

import (
	"fmt"
)

func createCoreTables() error {
	_, err := db.Exec(SQL.CreateUsersTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateUserAliasesTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateGmailTokensTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateMessagesTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateTaskTranslationsTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateTenantAliasesTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateScanMetadataTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateSlackThreadsTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateReportsTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateReportTranslationsTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateReportTranslationsIndex)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreatePromptLogsTable)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateAIInferenceLogsTable)
	return err
}

func runMigrations() error {
	//Why: Performs non-destructive column additions to the messages schema to support new features like categories and deadlines.
	_, _ = db.Exec(SQL.MigrateMessagesAddUserEmail)
	_, _ = db.Exec(SQL.MigrateMessagesAddIsDeleted)
	_, _ = db.Exec(SQL.MigrateMessagesAddRoom)
	_, _ = db.Exec(SQL.MigrateMessagesAddDone)
	_, _ = db.Exec(SQL.MigrateMessagesAddCompletedAt)
	_, _ = db.Exec(SQL.MigrateMessagesAddOriginalText)
	_, _ = db.Exec(SQL.MigrateMessagesAddCategory)
	_, _ = db.Exec(SQL.MigrateMessagesAddDeadline)
	_, _ = db.Exec(SQL.MigrateMessagesAddThreadID)
	_, _ = db.Exec(SQL.MigrateMessagesAddAssigneeReason)
	_, _ = db.Exec(SQL.MigrateMessagesAddRepliedToID)
	_, _ = db.Exec(SQL.MigrateMessagesAddIsContextQuery)
	_, _ = db.Exec(SQL.MigrateMessagesAddConstraints)
	_, _ = db.Exec(SQL.MigrateMessagesAddMetadata)
	_, _ = db.Exec(SQL.MigrateMessagesAddSourceChannels)
	_, _ = db.Exec(SQL.CreateIdxUserTS)

	//Why: Extends the users table with gamification-related metadata including points, levels, and streaks.
	_, _ = db.Exec(SQL.MigrateUsersAddPoints)
	_, _ = db.Exec(SQL.MigrateUsersAddStreak)
	_, _ = db.Exec(SQL.MigrateUsersAddLevel)
	_, _ = db.Exec(SQL.MigrateUsersAddXP)
	_, _ = db.Exec(SQL.MigrateUsersAddDailyGoal)
	_, _ = db.Exec(SQL.MigrateUsersAddLastCompletedAt)
	_, _ = db.Exec(SQL.MigrateUsersAddStreakFreezes)

	_, _ = db.Exec(SQL.MigrateAchievementsAddTargetValue)
	_, _ = db.Exec(SQL.MigrateAchievementsAddXPReward)
	_, _ = db.Exec(SQL.MigrateReportsAddIsTruncated)

	// Why: Handle migration for multi-language support (ISO 639-1) across both tasks and reports.
	_, _ = db.Exec(SQL.MigrateTaskTranslationsRenameLanguage)
	_, _ = db.Exec(SQL.MigrateReportTranslationsRenameLanguage)
	
	// Why: Fallback if rename failed or table was fresh.
	_, _ = db.Exec(SQL.MigrateTaskTranslationsAddLanguageCode)
	_, _ = db.Exec(SQL.MigrateReportTranslationsAddLanguageCode)

	migrateExistingData()
	// Why: Views are essential for report generation and message resolution.
	// Moving them here ensures they are always recreated/updated on every startup.
	if _, err := db.Exec(SQL.CreateContactsResolvedView); err != nil {
		return fmt.Errorf("failed to create contacts_resolved view: %w", err)
	}
	if _, err := db.Exec(SQL.CreateMessagesView); err != nil {
		return fmt.Errorf("failed to create messages view: %w", err)
	}
	if _, err := db.Exec(SQL.CreateUsersView); err != nil {
		return fmt.Errorf("failed to create users view: %w", err)
	}

	return nil
}

func migrateExistingData() {
	//Why: Normalizes existing data by ensuring critical fields are populated with valid default values.
	_, _ = db.Exec(SQL.MigrateDataNormalizeIsDeleted)
	_, _ = db.Exec(SQL.MigrateDataNormalizeRoom)
	_, _ = db.Exec(SQL.MigrateDataNormalizeCategoryWaiting)
	_, _ = db.Exec(SQL.MigrateDataNormalizeCategoryPromise)
}

func createIndexes() {
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
		SQL.CreateIdxMessagesUserDeletedCreated,
		SQL.CreateIdxMessagesUserDoneCompleted,
	}
	for _, idx := range indexes {
		if idx != "" {
			_, _ = db.Exec(idx)
		}
	}
}
