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

-- name: CreateIdxUserTS :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_ts ON messages(user_email, source_ts);

-- name: MigrateUsersAddPoints :exec
ALTER TABLE users ADD COLUMN points INTEGER DEFAULT 0;

-- name: MigrateUsersAddStreak :exec
ALTER TABLE users ADD COLUMN streak INTEGER DEFAULT 0;

-- name: MigrateUsersAddLevel :exec
ALTER TABLE users ADD COLUMN level INTEGER DEFAULT 1;

-- name: MigrateUsersAddXP :exec
ALTER TABLE users ADD COLUMN xp INTEGER DEFAULT 0;

-- name: MigrateUsersAddDailyGoal :exec
ALTER TABLE users ADD COLUMN daily_goal INTEGER DEFAULT 5;

-- name: MigrateUsersAddLastCompletedAt :exec
ALTER TABLE users ADD COLUMN last_completed_at DATETIME;

-- name: MigrateUsersAddStreakFreezes :exec
ALTER TABLE users ADD COLUMN streak_freezes INTEGER DEFAULT 0;

-- name: MigrateAchievementsAddTargetValue :exec
ALTER TABLE achievements ADD COLUMN target_value INTEGER DEFAULT 0;

-- name: MigrateAchievementsAddXPReward :exec
ALTER TABLE achievements ADD COLUMN xp_reward INTEGER DEFAULT 0;

-- name: MigrateReportsAddIsTruncated :exec
ALTER TABLE reports ADD COLUMN is_truncated INTEGER DEFAULT 0;

-- name: MigrateTaskTranslationsRenameLanguage :exec
ALTER TABLE task_translations RENAME COLUMN language TO language_code;

-- name: MigrateReportTranslationsRenameLanguage :exec
ALTER TABLE report_translations RENAME COLUMN language TO language_code;

-- name: MigrateTaskTranslationsAddLanguageCode :exec
ALTER TABLE task_translations ADD COLUMN language_code TEXT;

-- name: MigrateReportTranslationsAddLanguageCode :exec
ALTER TABLE report_translations ADD COLUMN language_code TEXT;

-- name: MigrateDataNormalizeIsDeleted :exec
UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL;

-- name: MigrateDataNormalizeRoom :exec
UPDATE messages SET room = '' WHERE room IS NULL;

-- name: MigrateDataNormalizeCategoryWaiting :exec
UPDATE messages SET category = 'waiting' WHERE task LIKE '[회신 대기]%';

-- name: MigrateDataNormalizeCategoryPromise :exec
UPDATE messages SET category = 'promise' WHERE task LIKE '[나의 약속]%';

-- name: CreateIdxMessagesTask :exec
CREATE INDEX IF NOT EXISTS idx_messages_task ON messages (task);

-- name: CreateIdxMessagesRoom :exec
CREATE INDEX IF NOT EXISTS idx_messages_room ON messages (room);

-- name: CreateIdxMessagesRequester :exec
CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages (requester);

-- name: CreateIdxMessagesAssignee :exec
CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages (assignee);

-- name: CreateIdxMessagesOriginalText :exec
CREATE INDEX IF NOT EXISTS idx_messages_original_text ON messages (original_text);

-- name: CreateIdxMessagesSource :exec
CREATE INDEX IF NOT EXISTS idx_messages_source ON messages (source);

-- name: CreateIdxMessagesCreatedAtDesc :exec
CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages (created_at DESC);

-- name: CreateIdxMessagesUserEmail :exec
CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages (user_email);

-- name: CreateIdxMessagesIsDeleted :exec
CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages (is_deleted);

-- name: CreateIdxMessagesCompletedAt :exec
CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages (completed_at) WHERE done = 1;

-- name: CreateIdxMessagesUserSourceTS :exec
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_user_source_ts ON messages(user_email, source_ts);

-- name: CreateIdxTaskTranslationsIDLangCode :exec
CREATE INDEX IF NOT EXISTS idx_task_translations_id_lang_code ON task_translations (language_code);

-- name: CreateIdxMessagesUserDeletedCreated :exec
CREATE INDEX IF NOT EXISTS idx_messages_user_deleted_created ON messages (user_email, is_deleted, created_at DESC);

-- name: CreateIdxMessagesUserDoneCompleted :exec
CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages (user_email, done, completed_at DESC);
