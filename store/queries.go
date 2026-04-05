package store

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed queries/*.sql
var queryFS embed.FS

// SQL defines all centralized SQL queries used in the application.
// This provides a single source of truth for all database operations.
var SQL = struct {
	//Why: Groups SQL queries for table schema definitions.
	CreateUsersTable            string
	CreateUserAliasesTable      string
	CreateGmailTokensTable      string
	CreateMessagesTable         string
	CreateTaskTranslationsTable string
	CreateTenantAliasesTable    string
	CreateScanMetadataTable     string
	CreateAchievementsTable     string
	CreateUserAchievementsTable string
	CreateContactsTable         string
	CreateContactsResolvedView  string
	CreateReportsTable          string
	CreateReportTranslationsTable string
	CreateReportTranslationsIndex string
	CreatePromptLogsTable         string
	CreateAIInferenceLogsTable     string
	InsertAIInferenceLog          string
	CreateMessagesView string
	CreateUsersView    string

	//Why: Groups SQL queries for message-related operations.
	SaveMessage                  string
	SaveMessagesBase             string
	MarkMessageDone              string
	UpdateTaskText               string
	UpdateTaskDescriptionAppend  string
	UpdateTaskFullAppend         string
	UpdateTaskAssignee           string
	UpdateTaskSourceChannels     string
	DeleteMessages               string
	HardDeleteMessages           string
	RestoreMessages              string
	GetMessageByID               string
	GetMessagesByIDs             string
	UpdateMessageCategory        string
	GetMessagesByEmail           string
	GetIncompleteByThreadID      string
	GetArchivedMessagesCountBase string
	GetArchivedMessagesBase      string
	RefreshCacheActive           string
	RefreshCacheArchive          string
	ArchiveOldTasks              string
	GetActiveTasksForContext     string
	UpdateCategoryMerged         string
	GetMessagesForMerge          string

	//Why: Groups SQL queries for user-related operations.
	GetAllUsers            string
	GetUserByEmail         string
	GetUserByID            string
	GetUserByEmailSimple   string
	CreateUser             string
	CreateUserReturningAll string
	UpdateUserNamePicture  string
	UpdateUserWAJID        string
	UpdateUserSlackID      string

	//Why: Groups SQL queries for token and credential management.
	InitTokenUsageTable  string
	UpsertTokenUsage     string
	GetDailyTokenUsage   string
	GetMonthlyTokenUsage string
	UpsertGmailToken     string
	GetGmailToken        string
	DeleteGmailToken     string

	//Why: Groups SQL queries for contact and mapping management.
	AddContactMapping string
	UpsertContactMapping string
	DeleteContactMapping string

	//Why: Groups SQL queries for statistics and productivity metrics.
	GetTotalCompleted      string
	GetPendingMe           string
	GetDailyGoal           string
	GetDailyCompletions    string
	GetHourlyActivity      string
	GetAbandonedTasks      string
	GetSourceDistributionActive  string
	GetSourceDistributionTotal   string
	GetCompletionHistory         string
	GetEarlyBirdCompleted  string
	GetMaxDailyCompleted   string
	GetWaitingTasks        string
	GetPendingOthers       string

	//Why: Groups SQL queries for user and tenant aliases.
	UpsertTenantAlias   string
	DeleteTenantAlias   string
	GetUserAliases      string
	CreateUserAlias     string
	DeleteUserAlias     string
	GetAllTenantAliases string
	GetAllUserAliases   string

	//Why: Groups SQL queries for background scanning and metadata.
	LoadUsersSimple               string
	LoadUserAliasesAll            string
	LoadScanMetadataAll           string
	LoadGmailTokensAll            string
	LoadTenantAliasesAll          string
	LoadContactsAll               string
	UpsertScanMetadata            string
	DeleteScanMetadataSlackThread string

	//Why: Groups SQL queries for Slack thread tracking and management.
	CreateSlackThreadsTable string
	GetActiveSlackThreadsNew  string
	UpsertSlackThread        string
	CloseSlackThread         string

	//Why: Groups SQL queries for task translation management.
	GetTaskTranslation       string
	GetTaskTranslationsBatch string
	UpsertTaskTranslation    string

	//Why: Groups SQL queries for gamification and achievements.
	UpdateUserGamification string
	GetAchievements        string
	GetUserAchievements    string
	UnlockAchievement      string
	InsertPromptLog        string

	//Why: Groups SQL queries for report-related operations.
	InsertReport         string
	InsertReportTranslation string
	GetReport            string
	GetReportByDate      string
	ListReports          string
	GetReportByID        string
	GetReportTranslations string
	GetReportList          string
	DeleteReport         string
	GetMessagesForReport string

	//Why: Self-Healing Identity Resolution queries.
	GetContactByIdentifier string
	UpdateMessageIdentity  string

	//Why: Account Linking specific queries.
	SearchContacts    string
	UpdateContactLink string
	UnlinkContact     string
	FlattenChildren   string
	GetLinkedContacts string

	//Why: SQL Migration scripts extracted from migrations.go.
	MigrateMessagesAddUserEmail        string
	MigrateMessagesAddIsDeleted        string
	MigrateMessagesAddRoom             string
	MigrateMessagesAddDone             string
	MigrateMessagesAddCompletedAt      string
	MigrateMessagesAddOriginalText     string
	MigrateMessagesAddCategory         string
	MigrateMessagesAddDeadline         string
	MigrateMessagesAddThreadID         string
	MigrateMessagesAddAssigneeReason   string
	MigrateMessagesAddRepliedToID      string
	MigrateMessagesAddIsContextQuery   string
	MigrateMessagesAddConstraints      string
	MigrateMessagesAddMetadata         string
	MigrateMessagesAddSourceChannels   string
	CreateIdxUserTS                    string
	MigrateUsersAddPoints              string
	MigrateUsersAddStreak              string
	MigrateUsersAddLevel               string
	MigrateUsersAddXP                  string
	MigrateUsersAddDailyGoal           string
	MigrateUsersAddLastCompletedAt     string
	MigrateUsersAddStreakFreezes       string
	MigrateAchievementsAddTargetValue  string
	MigrateAchievementsAddXPReward     string
	MigrateReportsAddIsTruncated       string
	MigrateTaskTranslationsRenameLanguage string
	MigrateReportTranslationsRenameLanguage string
	MigrateTaskTranslationsAddLanguageCode string
	MigrateReportTranslationsAddLanguageCode string
	MigrateDataNormalizeIsDeleted      string
	MigrateDataNormalizeRoom           string
	MigrateDataNormalizeCategoryWaiting string
	MigrateDataNormalizeCategoryPromise string
	CreateIdxMessagesTask              string
	CreateIdxMessagesRoom              string
	CreateIdxMessagesRequester         string
	CreateIdxMessagesAssignee          string
	CreateIdxMessagesOriginalText      string
	CreateIdxMessagesSource            string
	CreateIdxMessagesCreatedAtDesc     string
	CreateIdxMessagesUserEmail         string
	CreateIdxMessagesIsDeleted         string
	CreateIdxMessagesCompletedAt       string
	CreateIdxMessagesUserSourceTS      string
	CreateIdxTaskTranslationsIDLangCode string
	CreateIdxMessagesUserDeletedCreated string
	CreateIdxMessagesUserDoneCompleted string

	//Why: Achievement seeding and validation.
	GetAchievementCount    string
	DeleteAllAchievements  string
	SeedAchievements      string
}{}

func init() {
	if err := loadAllQueries(); err != nil {
		panic(fmt.Sprintf("failed to load SQL queries: %v", err))
	}
}

func loadAllQueries() error {
	queries := make(map[string]string)
	entries, err := queryFS.ReadDir("queries")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := queryFS.ReadFile("queries/" + entry.Name())
		if err != nil {
			return err
		}

		lines := strings.Split(string(content), "\n")
		var currentName string
		var currentQuery strings.Builder

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "-- name: ") {
				if currentName != "" {
					queries[currentName] = strings.TrimSpace(currentQuery.String())
					currentQuery.Reset()
				}
				parts := strings.Split(strings.TrimPrefix(trimmed, "-- name: "), " ")
				currentName = parts[0]
			} else if strings.HasPrefix(trimmed, "--") {
				continue
			} else if currentName != "" {
				currentQuery.WriteString(line + "\n")
			}
		}
		if currentName != "" {
			queries[currentName] = strings.TrimSpace(currentQuery.String())
		}
	}

	//Why: Uses manual assignment instead of reflection for better safety and performance at this scale.
	SQL.CreateUsersTable = queries["CreateUsersTable"]
	SQL.CreateUserAliasesTable = queries["CreateUserAliasesTable"]
	SQL.CreateGmailTokensTable = queries["CreateGmailTokensTable"]
	SQL.CreateMessagesTable = queries["CreateMessagesTable"]
	SQL.CreateTaskTranslationsTable = queries["CreateTaskTranslationsTable"]
	SQL.CreateTenantAliasesTable = queries["CreateTenantAliasesTable"]
	SQL.CreateScanMetadataTable = queries["CreateScanMetadataTable"]
	SQL.CreateAchievementsTable = queries["CreateAchievementsTable"]
	SQL.CreateUserAchievementsTable = queries["CreateUserAchievementsTable"]
	SQL.CreateContactsTable = queries["CreateContactsTable"]
	SQL.CreateContactsResolvedView = queries["CreateContactsResolvedView"]
	SQL.CreateReportsTable = queries["CreateReportsTable"]
	SQL.CreateReportTranslationsTable = queries["CreateReportTranslationsTable"]
	SQL.CreateReportTranslationsIndex = queries["CreateReportTranslationsIndex"]
	SQL.CreatePromptLogsTable = queries["CreatePromptLogsTable"]
	SQL.CreateAIInferenceLogsTable = queries["CreateAIInferenceLogsTable"]
	SQL.InsertAIInferenceLog = queries["InsertAIInferenceLog"]
	SQL.CreateMessagesView = queries["CreateMessagesView"]
	SQL.CreateUsersView = queries["CreateUsersView"]

	SQL.SaveMessage = queries["SaveMessage"]
	SQL.SaveMessagesBase = queries["SaveMessagesBase"]
	SQL.MarkMessageDone = queries["MarkMessageDone"]
	SQL.UpdateTaskText = queries["UpdateTaskText"]
	SQL.UpdateTaskDescriptionAppend = queries["UpdateTaskDescriptionAppend"]
	SQL.UpdateTaskFullAppend = queries["UpdateTaskFullAppend"]
	SQL.UpdateTaskAssignee = queries["UpdateTaskAssignee"]
	SQL.UpdateTaskSourceChannels = queries["UpdateTaskSourceChannels"]
	SQL.DeleteMessages = queries["DeleteMessages"]
	SQL.HardDeleteMessages = queries["HardDeleteMessages"]
	SQL.RestoreMessages = queries["RestoreMessages"]
	SQL.GetMessageByID = queries["GetMessageByID"]
	SQL.GetMessagesByIDs = queries["GetMessagesByIDs"]
	SQL.UpdateMessageCategory = queries["UpdateMessageCategory"]
	SQL.GetMessagesByEmail = queries["GetMessagesByEmail"]
	SQL.GetIncompleteByThreadID = queries["GetIncompleteByThreadID"]
	SQL.GetArchivedMessagesCountBase = queries["GetArchivedMessagesCountBase"]
	SQL.GetArchivedMessagesBase = queries["GetArchivedMessagesBase"]
	SQL.RefreshCacheActive = queries["RefreshCacheActive"]
	SQL.RefreshCacheArchive = queries["RefreshCacheArchive"]
	SQL.ArchiveOldTasks = queries["ArchiveOldTasks"]
	SQL.GetActiveTasksForContext = queries["GetActiveTasksForContext"]
	SQL.UpdateCategoryMerged = queries["UpdateCategoryMerged"]
	SQL.GetMessagesForMerge = queries["GetMessagesForMerge"]

	SQL.GetAllUsers = queries["GetAllUsers"]
	SQL.GetUserByEmail = queries["GetUserByEmail"]
	SQL.GetUserByID = queries["GetUserByID"]
	SQL.GetUserByEmailSimple = queries["GetUserByEmailSimple"]
	SQL.CreateUser = queries["CreateUser"]
	SQL.CreateUserReturningAll = queries["CreateUserReturningAll"]
	SQL.UpdateUserNamePicture = queries["UpdateUserNamePicture"]
	SQL.UpdateUserWAJID = queries["UpdateUserWAJID"]
	SQL.UpdateUserSlackID = queries["UpdateUserSlackID"]

	SQL.InitTokenUsageTable = queries["InitTokenUsageTable"]
	SQL.UpsertTokenUsage = queries["UpsertTokenUsage"]
	SQL.GetDailyTokenUsage = queries["GetDailyTokenUsage"]
	SQL.GetMonthlyTokenUsage = queries["GetMonthlyTokenUsage"]
	SQL.UpsertGmailToken = queries["UpsertGmailToken"]
	SQL.GetGmailToken = queries["GetGmailToken"]
	SQL.DeleteGmailToken = queries["DeleteGmailToken"]

	SQL.AddContactMapping = queries["AddContactMapping"]
	SQL.UpsertContactMapping = queries["UpsertContactMapping"]
	SQL.DeleteContactMapping = queries["DeleteContactMapping"]

	SQL.GetTotalCompleted = queries["GetTotalCompleted"]
	SQL.GetPendingMe = queries["GetPendingMe"]
	SQL.GetDailyGoal = queries["GetDailyGoal"]
	SQL.GetDailyCompletions = queries["GetDailyCompletions"]
	SQL.GetHourlyActivity = queries["GetHourlyActivity"]
	SQL.GetAbandonedTasks = queries["GetAbandonedTasks"]
	SQL.GetSourceDistributionActive = queries["GetSourceDistributionActive"]
	SQL.GetSourceDistributionTotal = queries["GetSourceDistributionTotal"]
	SQL.GetCompletionHistory = queries["GetCompletionHistory"]
	SQL.GetEarlyBirdCompleted = queries["GetEarlyBirdCompleted"]
	SQL.GetMaxDailyCompleted = queries["GetMaxDailyCompleted"]
	SQL.GetWaitingTasks = queries["GetWaitingTasks"]
	SQL.GetPendingOthers = queries["GetPendingOthers"]

	SQL.UpsertTenantAlias = queries["UpsertTenantAlias"]
	SQL.DeleteTenantAlias = queries["DeleteTenantAlias"]
	SQL.GetUserAliases = queries["GetUserAliases"]
	SQL.CreateUserAlias = queries["CreateUserAlias"]
	SQL.DeleteUserAlias = queries["DeleteUserAlias"]
	SQL.GetAllTenantAliases = queries["GetAllTenantAliases"]
	SQL.GetAllUserAliases = queries["GetAllUserAliases"]

	SQL.LoadUsersSimple = queries["LoadUsersSimple"]
	SQL.LoadUserAliasesAll = queries["LoadUserAliasesAll"]
	SQL.LoadScanMetadataAll = queries["LoadScanMetadataAll"]
	SQL.LoadGmailTokensAll = queries["LoadGmailTokensAll"]
	SQL.LoadTenantAliasesAll = queries["LoadTenantAliasesAll"]
	SQL.LoadContactsAll = queries["LoadContactsAll"]
	SQL.UpsertScanMetadata = queries["UpsertScanMetadata"]
	SQL.DeleteScanMetadataSlackThread = queries["DeleteScanMetadataSlackThread"]

	SQL.CreateSlackThreadsTable = queries["CreateSlackThreadsTable"]
	SQL.GetActiveSlackThreadsNew = queries["GetActiveSlackThreadsNew"]
	SQL.UpsertSlackThread = queries["UpsertSlackThread"]
	SQL.CloseSlackThread = queries["CloseSlackThread"]

	SQL.GetTaskTranslation = queries["GetTaskTranslation"]
	SQL.GetTaskTranslationsBatch = queries["GetTaskTranslationsBatch"]
	SQL.UpsertTaskTranslation = queries["UpsertTaskTranslation"]

	SQL.UpdateUserGamification = queries["UpdateUserGamification"]
	SQL.GetAchievements = queries["GetAchievements"]
	SQL.GetUserAchievements = queries["GetUserAchievements"]
	SQL.UnlockAchievement = queries["UnlockAchievement"]
	SQL.InsertPromptLog = queries["InsertPromptLog"]

	SQL.InsertReport = queries["InsertReport"]
	SQL.InsertReportTranslation = queries["InsertReportTranslation"]
	SQL.GetReport = queries["GetReport"]
	SQL.GetReportByDate = queries["GetReportByDate"]
	SQL.ListReports = queries["ListReports"]
	SQL.GetReportByID = queries["GetReportByID"]
	SQL.GetReportTranslations = queries["GetReportTranslations"]
	SQL.GetReportList = queries["GetReportList"]
	SQL.DeleteReport = queries["DeleteReport"]
	SQL.GetMessagesForReport = queries["GetMessagesForReport"]

	SQL.GetContactByIdentifier = queries["GetContactByIdentifier"]
	SQL.UpdateMessageIdentity = queries["UpdateMessageIdentity"]

	SQL.SearchContacts = queries["SearchContacts"]
	SQL.UpdateContactLink = queries["UpdateContactLink"]
	SQL.UnlinkContact = queries["UnlinkContact"]
	SQL.FlattenChildren = queries["FlattenChildren"]
	SQL.GetLinkedContacts = queries["GetLinkedContacts"]

	SQL.MigrateMessagesAddUserEmail = queries["MigrateMessagesAddUserEmail"]
	SQL.MigrateMessagesAddIsDeleted = queries["MigrateMessagesAddIsDeleted"]
	SQL.MigrateMessagesAddRoom = queries["MigrateMessagesAddRoom"]
	SQL.MigrateMessagesAddDone = queries["MigrateMessagesAddDone"]
	SQL.MigrateMessagesAddCompletedAt = queries["MigrateMessagesAddCompletedAt"]
	SQL.MigrateMessagesAddOriginalText = queries["MigrateMessagesAddOriginalText"]
	SQL.MigrateMessagesAddCategory = queries["MigrateMessagesAddCategory"]
	SQL.MigrateMessagesAddDeadline = queries["MigrateMessagesAddDeadline"]
	SQL.MigrateMessagesAddThreadID = queries["MigrateMessagesAddThreadID"]
	SQL.MigrateMessagesAddAssigneeReason = queries["MigrateMessagesAddAssigneeReason"]
	SQL.MigrateMessagesAddRepliedToID = queries["MigrateMessagesAddRepliedToID"]
	SQL.MigrateMessagesAddIsContextQuery = queries["MigrateMessagesAddIsContextQuery"]
	SQL.MigrateMessagesAddConstraints = queries["MigrateMessagesAddConstraints"]
	SQL.MigrateMessagesAddMetadata = queries["MigrateMessagesAddMetadata"]
	SQL.MigrateMessagesAddSourceChannels = queries["MigrateMessagesAddSourceChannels"]
	SQL.CreateIdxUserTS = queries["CreateIdxUserTS"]
	SQL.MigrateUsersAddPoints = queries["MigrateUsersAddPoints"]
	SQL.MigrateUsersAddStreak = queries["MigrateUsersAddStreak"]
	SQL.MigrateUsersAddLevel = queries["MigrateUsersAddLevel"]
	SQL.MigrateUsersAddXP = queries["MigrateUsersAddXP"]
	SQL.MigrateUsersAddDailyGoal = queries["MigrateUsersAddDailyGoal"]
	SQL.MigrateUsersAddLastCompletedAt = queries["MigrateUsersAddLastCompletedAt"]
	SQL.MigrateUsersAddStreakFreezes = queries["MigrateUsersAddStreakFreezes"]
	SQL.MigrateAchievementsAddTargetValue = queries["MigrateAchievementsAddTargetValue"]
	SQL.MigrateAchievementsAddXPReward = queries["MigrateAchievementsAddXPReward"]
	SQL.MigrateReportsAddIsTruncated = queries["MigrateReportsAddIsTruncated"]
	SQL.MigrateTaskTranslationsRenameLanguage = queries["MigrateTaskTranslationsRenameLanguage"]
	SQL.MigrateReportTranslationsRenameLanguage = queries["MigrateReportTranslationsRenameLanguage"]
	SQL.MigrateTaskTranslationsAddLanguageCode = queries["MigrateTaskTranslationsAddLanguageCode"]
	SQL.MigrateReportTranslationsAddLanguageCode = queries["MigrateReportTranslationsAddLanguageCode"]
	SQL.MigrateDataNormalizeIsDeleted = queries["MigrateDataNormalizeIsDeleted"]
	SQL.MigrateDataNormalizeRoom = queries["MigrateDataNormalizeRoom"]
	SQL.MigrateDataNormalizeCategoryWaiting = queries["MigrateDataNormalizeCategoryWaiting"]
	SQL.MigrateDataNormalizeCategoryPromise = queries["MigrateDataNormalizeCategoryPromise"]
	SQL.CreateIdxMessagesTask = queries["CreateIdxMessagesTask"]
	SQL.CreateIdxMessagesRoom = queries["CreateIdxMessagesRoom"]
	SQL.CreateIdxMessagesRequester = queries["CreateIdxMessagesRequester"]
	SQL.CreateIdxMessagesAssignee = queries["CreateIdxMessagesAssignee"]
	SQL.CreateIdxMessagesOriginalText = queries["CreateIdxMessagesOriginalText"]
	SQL.CreateIdxMessagesSource = queries["CreateIdxMessagesSource"]
	SQL.CreateIdxMessagesCreatedAtDesc = queries["CreateIdxMessagesCreatedAtDesc"]
	SQL.CreateIdxMessagesUserEmail = queries["CreateIdxMessagesUserEmail"]
	SQL.CreateIdxMessagesIsDeleted = queries["CreateIdxMessagesIsDeleted"]
	SQL.CreateIdxMessagesCompletedAt = queries["CreateIdxMessagesCompletedAt"]
	SQL.CreateIdxMessagesUserSourceTS = queries["CreateIdxMessagesUserSourceTS"]
	SQL.CreateIdxTaskTranslationsIDLangCode = queries["CreateIdxTaskTranslationsIDLangCode"]
	SQL.CreateIdxMessagesUserDeletedCreated = queries["CreateIdxMessagesUserDeletedCreated"]
	SQL.CreateIdxMessagesUserDoneCompleted = queries["CreateIdxMessagesUserDoneCompleted"]

	SQL.GetAchievementCount = queries["GetAchievementCount"]
	SQL.DeleteAllAchievements = queries["DeleteAllAchievements"]
	SQL.SeedAchievements = queries["SeedAchievements"]

	// Why: Validate critical startup queries to prevent silent runtime failures that are difficult to debug.
	if err := validateCriticalQueries(); err != nil {
		return err
	}

	return nil
}

func validateCriticalQueries() error {
	critical := map[string]string{
		"SaveMessage":         SQL.SaveMessage,
		"CreateMessagesTable": SQL.CreateMessagesTable,
		"CreateMessagesView":  SQL.CreateMessagesView,
		"GetMessagesByIDs":    SQL.GetMessagesByIDs,
		"GetUserByEmail":      SQL.GetUserByEmail,
	}

	for name, query := range critical {
		if query == "" {
			return fmt.Errorf("critical SQL query %q is missing (check -- name: tag in .sql files)", name)
		}
	}
	return nil
}
