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
	CreateReportsTable          string

	//Why: Groups SQL queries for database view definitions.
	CreateMessagesView string
	CreateUsersView    string

	//Why: Groups SQL queries for message-related operations.
	SaveMessage                  string
	SaveMessagesBase             string
	MarkMessageDone              string
	UpdateTaskText               string
	UpdateTaskAssignee           string
	DeleteMessages               string
	HardDeleteMessages           string
	RestoreMessages              string
	GetMessageByID               string
	GetMessagesByIDs             string
	GetMessagesByEmail           string
	GetIncompleteByThreadID      string
	GetArchivedMessagesCountBase string
	GetArchivedMessagesBase      string
	RefreshCacheActive           string
	RefreshCacheArchive          string
	ArchiveOldTasks              string

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

	//Why: Groups SQL queries for report-related operations.
	UpsertReport         string
	GetReport            string
	GetMessagesForReport string
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
	SQL.CreateReportsTable = queries["CreateReportsTable"]
	SQL.CreateMessagesView = queries["CreateMessagesView"]
	SQL.CreateUsersView = queries["CreateUsersView"]

	SQL.SaveMessage = queries["SaveMessage"]
	SQL.SaveMessagesBase = queries["SaveMessagesBase"]
	SQL.MarkMessageDone = queries["MarkMessageDone"]
	SQL.UpdateTaskText = queries["UpdateTaskText"]
	SQL.UpdateTaskAssignee = queries["UpdateTaskAssignee"]
	SQL.DeleteMessages = queries["DeleteMessages"]
	SQL.HardDeleteMessages = queries["HardDeleteMessages"]
	SQL.RestoreMessages = queries["RestoreMessages"]
	SQL.GetMessageByID = queries["GetMessageByID"]
	SQL.GetMessagesByIDs = queries["GetMessagesByIDs"]
	SQL.GetMessagesByEmail = queries["GetMessagesByEmail"]
	SQL.GetIncompleteByThreadID = queries["GetIncompleteByThreadID"]
	SQL.GetArchivedMessagesCountBase = queries["GetArchivedMessagesCountBase"]
	SQL.GetArchivedMessagesBase = queries["GetArchivedMessagesBase"]
	SQL.RefreshCacheActive = queries["RefreshCacheActive"]
	SQL.RefreshCacheArchive = queries["RefreshCacheArchive"]
	SQL.ArchiveOldTasks = queries["ArchiveOldTasks"]

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

	SQL.UpsertReport = queries["UpsertReport"]
	SQL.GetReport = queries["GetReport"]
	SQL.GetMessagesForReport = queries["GetMessagesForReport"]

	return nil
}
