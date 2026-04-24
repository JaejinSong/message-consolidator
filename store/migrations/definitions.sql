-- name: MigrateMessagesAddUserEmail :exec
ALTER TABLE messages ADD COLUMN user_email TEXT;

-- name: MigrateMessagesAddIsDeleted :exec
ALTER TABLE messages ADD COLUMN is_deleted BOOLEAN DEFAULT 0;

-- name: MigrateMessagesAddRoom :exec
ALTER TABLE messages ADD COLUMN room TEXT;

-- name: MigrateMessagesAddDone :exec
ALTER TABLE messages ADD COLUMN done BOOLEAN DEFAULT 0;

-- name: MigrateMessagesAddCompletedAt :exec
ALTER TABLE messages ADD COLUMN completed_at DATETIME;

-- name: MigrateMessagesAddOriginalText :exec
ALTER TABLE messages ADD COLUMN original_text TEXT;

-- name: MigrateMessagesAddCategory :exec
ALTER TABLE messages ADD COLUMN category TEXT DEFAULT 'todo';

-- name: MigrateMessagesAddDeadline :exec
ALTER TABLE messages ADD COLUMN deadline TEXT;

-- name: MigrateMessagesAddThreadID :exec
ALTER TABLE messages ADD COLUMN thread_id TEXT;

-- name: MigrateMessagesAddAssigneeReason :exec
ALTER TABLE messages ADD COLUMN assignee_reason TEXT;

-- name: MigrateMessagesAddRepliedToID :exec
ALTER TABLE messages ADD COLUMN replied_to_id TEXT;

-- name: MigrateMessagesAddIsContextQuery :exec
ALTER TABLE messages ADD COLUMN is_context_query INTEGER DEFAULT 0;

-- name: MigrateMessagesAddConstraints :exec
ALTER TABLE messages ADD COLUMN constraints TEXT DEFAULT '[]';

-- name: MigrateMessagesAddMetadata :exec
ALTER TABLE messages ADD COLUMN metadata TEXT DEFAULT '{}';

-- name: MigrateMessagesAddSourceChannels :exec
ALTER TABLE messages ADD COLUMN source_channels TEXT DEFAULT '[]';

-- name: MigrateMessagesAddConsolidatedContext :exec
ALTER TABLE messages ADD COLUMN consolidated_context TEXT DEFAULT '[]';

-- name: MigrateMessagesAddPinned :exec
ALTER TABLE messages ADD COLUMN pinned BOOLEAN DEFAULT FALSE;

-- name: CreateIdxUserTS :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_source_ts ON messages(user_email, source_ts);

-- name: MigrateUsersAddPoints :exec
ALTER TABLE users ADD COLUMN points INTEGER DEFAULT 0;

-- name: MigrateUsersAddStreak :exec
ALTER TABLE users ADD COLUMN streak INTEGER DEFAULT 0;

-- name: MigrateUsersAddLevel :exec
ALTER TABLE users ADD COLUMN level INTEGER DEFAULT 1;

-- name: MigrateUsersAddXP :exec
ALTER TABLE users ADD COLUMN xp INTEGER DEFAULT 10;

-- name: MigrateUsersAddDailyGoal :exec
ALTER TABLE users ADD COLUMN daily_goal INTEGER DEFAULT 5;

-- name: MigrateUsersAddLastCompletedAt :exec
ALTER TABLE users ADD COLUMN last_completed_at DATETIME;

-- name: MigrateUsersAddStreakFreezes :exec
ALTER TABLE users ADD COLUMN streak_freezes INTEGER DEFAULT 0;

-- name: MigrateReportsAddIsTruncated :exec
ALTER TABLE reports ADD COLUMN is_truncated INTEGER DEFAULT 0;

-- name: MigrateTaskTranslationsRenameLanguage :exec
ALTER TABLE task_translations RENAME COLUMN language TO language_deprecated;

-- name: MigrateTaskTranslationsAddLanguageCode :exec
ALTER TABLE task_translations ADD COLUMN language_code TEXT;

-- name: CreateIdxTaskTranslationsIDLangCode :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_task_trans_id_lang ON task_translations(message_id, language_code);

-- name: MigrateReportTranslationsRenameLanguage :exec
ALTER TABLE report_translations RENAME COLUMN language TO language_deprecated;

-- name: MigrateReportTranslationsAddLanguageCode :exec
ALTER TABLE report_translations ADD COLUMN language_code TEXT;

-- name: CreateReportTranslationsIndex :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_report_translations_report_id_lang ON report_translations(report_id, language_code);

-- name: MigrateDataNormalizeIsDeleted :exec
UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL;

-- name: MigrateDataNormalizeRoom :exec
UPDATE messages SET room = 'General' WHERE room IS NULL OR room = '';

-- name: MigrateDataNormalizeCategoryWaiting :exec
UPDATE messages SET category = 'todo' WHERE category = 'waiting';

-- name: MigrateDataNormalizeCategoryPromise :exec
UPDATE messages SET category = 'todo' WHERE category = 'promise';

-- name: CreateIdxMessagesTask :exec
CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task);

-- name: CreateIdxMessagesRoom :exec
CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room);

-- name: CreateIdxMessagesRequester :exec
CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages(requester);

-- name: CreateIdxMessagesAssignee :exec
CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages(assignee);

-- name: CreateIdxMessagesOriginalText :exec
CREATE INDEX IF NOT EXISTS idx_messages_original_text ON messages(original_text);

-- name: CreateIdxMessagesSource :exec
CREATE INDEX IF NOT EXISTS idx_messages_source ON messages(source);

-- name: CreateIdxMessagesCreatedAtDesc :exec
CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages(created_at DESC);

-- name: CreateIdxMessagesUserEmail :exec
CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages(user_email);

-- name: CreateIdxMessagesIsDeleted :exec
CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages(is_deleted);

-- name: CreateIdxMessagesCompletedAt :exec
CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages(completed_at);

-- name: CreateIdxMessagesUserSourceTS :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_user_source_ts ON messages(user_email, source_ts);

-- name: CreateIdxMessagesUserDoneCompleted :exec
CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages(user_email, done, completed_at);

-- name: MigrateContactsAddContactType :exec
ALTER TABLE contacts ADD COLUMN contact_type TEXT DEFAULT 'none';

-- name: MigrateContactsRenameLegacyAliases :exec
ALTER TABLE contacts RENAME COLUMN aliases TO legacy_aliases;

-- name: MigrateLegacyAliases :exec
INSERT INTO contact_aliases (contact_id, identifier_type, identifier_value, source)
SELECT id, 'legacy', legacy_aliases, source FROM contacts WHERE legacy_aliases IS NOT NULL AND legacy_aliases != '';

-- name: MigrateContactsDropLegacyAliases :exec
ALTER TABLE contacts DROP COLUMN legacy_aliases;

-- name: MigrateAchievementsAddTargetValue :exec
ALTER TABLE achievements ADD COLUMN target_value INTEGER DEFAULT 1;

-- name: MigrateAchievementsAddXPReward :exec
ALTER TABLE achievements ADD COLUMN xp_reward INTEGER DEFAULT 10;

-- name: MigrateUsersAddTgUserID :exec
ALTER TABLE users ADD COLUMN tg_user_id TEXT DEFAULT '';

-- name: CreateTelegramSessions :exec
CREATE TABLE IF NOT EXISTS telegram_sessions (
    email        TEXT PRIMARY KEY,
    session_data BLOB NOT NULL,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- name: CreateTelegramCredentials :exec
CREATE TABLE IF NOT EXISTS telegram_credentials (
    email      TEXT PRIMARY KEY,
    app_id     INTEGER NOT NULL,
    app_hash   TEXT NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
