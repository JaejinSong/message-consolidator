package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"net/url"
	"strings"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
	_ "modernc.org/sqlite"
)

var (
	db *sql.DB
)

func InitDB(cfg *config.Config) error {
	var err error
	dbURL := cfg.TursoURL
	authToken := cfg.TursoToken

	//Why: Handles remote-only Turso connections using the libsql:// prefix to ensure proper authentication.
	if strings.HasPrefix(dbURL, "libsql://") && authToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	}

	//Why: Configures embedded replicas to support local edge synchronization via the file: prefix and SyncURL settings.
	if strings.HasPrefix(dbURL, "file:") && cfg.TursoSyncURL != "" {
		u, parseErr := url.Parse(dbURL)
		if parseErr == nil {
			q := u.Query()
			q.Set("sync_url", cfg.TursoSyncURL)
			if authToken != "" {
				q.Set("authToken", authToken)
			}
			if cfg.TursoSyncInterval != "" {
				q.Set("sync_interval", cfg.TursoSyncInterval)
			}
			u.RawQuery = q.Encode()
			dbURL = u.String()
			logger.Infof("[DB] Embedded Replica mode enabled: %s (Sync with %s)", u.Path, cfg.TursoSyncURL)
		}
	}

	db, err = sql.Open("libsql", dbURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	setupConnectionPool(dbURL)

	// Why: Perform critical schema migrations (e.g., adding language_code) BEFORE creating tables or views
	// that might refer to these columns, preventing "no such column" errors during initialization.
	if err := runMigrations(); err != nil {
		return fmt.Errorf("pre-migration failed: %w", err)
	}

	if err := createCoreTables(); err != nil {
		logger.Warnf("[DB-INIT] Core table creation partially completed: %v", err)
	}

	if err := setupGamification(); err != nil {
		return fmt.Errorf("gamification setup failed: %w", err)
	}

	// Why: Perform critical schema migrations (e.g., adding language_code) to sync existing data
	// with new multi-language support (ISO 639-1). Errors are ignored to ensure idempotent startup.
	if err := runMigrations(); err != nil {
		logger.Warnf("[DB-INIT] Non-destructive migrations warning: %v", err)
	}

	createIndexes()

	return RefreshAllCaches()
}

func setupConnectionPool(connStr string) {
	//Why: Configures connection pool settings to prevent "stream is closed" or "bad connection" errors in serverless environments like Turso.
	idleConns := 2
	if strings.HasPrefix(connStr, "libsql://") {
		//Why: Disables idle connections for remote Turso environments to prevent holding onto stale connections dropped by the server.
		logger.Infof("[DB] Turso detected. Setting MaxIdleConns to 0, MaxOpenConns to 20.")
		idleConns = 0
	} else {
		logger.Infof("[DB] SQLite (Local) detected. Setting MaxIdleConns to 2, MaxOpenConns to 10.")
	}
	db.SetMaxIdleConns(idleConns)
	db.SetMaxOpenConns(20)
	db.SetConnMaxLifetime(1 * time.Minute)
	if idleConns > 0 {
		db.SetConnMaxIdleTime(30 * time.Second)
	}
}

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
	_, err = db.Exec(SQL.CreateContactsResolvedView)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateMessagesView)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateUsersView)
	return err
}

func runMigrations() error {
	//Why: Performs non-destructive column additions to the messages schema to support new features like categories and deadlines.
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN user_email TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN is_deleted BOOLEAN DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN room TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN done BOOLEAN DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN completed_at DATETIME;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN original_text TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN category TEXT DEFAULT 'todo';")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN deadline TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN thread_id TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN assignee_reason TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN replied_to_id TEXT;")

	//Why: Extends the users table with gamification-related metadata including points, levels, and streaks.
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN points INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN streak INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN level INTEGER DEFAULT 1;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN xp INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN daily_goal INTEGER DEFAULT 5;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN last_completed_at DATETIME;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN streak_freezes INTEGER DEFAULT 0;")

	_, _ = db.Exec("ALTER TABLE achievements ADD COLUMN target_value INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE achievements ADD COLUMN xp_reward INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE reports ADD COLUMN is_truncated INTEGER DEFAULT 0;")

	// Why: Handle migration for multi-language support (ISO 639-1) across both tasks and reports.
	// We use direct column rename if 'language' exists, or add 'language_code' if missing.
	_, _ = db.Exec("ALTER TABLE task_translations RENAME COLUMN language TO language_code;")
	_, _ = db.Exec("ALTER TABLE report_translations RENAME COLUMN language TO language_code;")
	
	// Why: If the table was newly created with 'language_code', above rename might fail or be unnecessary.
	// We ensure 'language_code' exists as a non-destructive fallback if rename didn't catch it.
	_, _ = db.Exec("ALTER TABLE task_translations ADD COLUMN language_code TEXT;")
	_, _ = db.Exec("ALTER TABLE report_translations ADD COLUMN language_code TEXT;")

	migrateExistingData()
	return nil
}

func migrateExistingData() {
	//Why: Normalizes existing data by ensuring critical fields are populated with valid default values.
	_, _ = db.Exec("UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL;")
	_, _ = db.Exec("UPDATE messages SET room = '' WHERE room IS NULL;")
	_, _ = db.Exec("UPDATE messages SET category = 'waiting' WHERE task LIKE '[회신 대기]%';")
	_, _ = db.Exec("UPDATE messages SET category = 'promise' WHERE task LIKE '[나의 약속]%';")

	//Why: Skips complex constraint migrations due to SQLite's limited support for standard ALTER TABLE operations.
}

func setupGamification() error {
	if _, err := db.Exec(SQL.CreateAchievementsTable); err != nil {
		logger.Errorf("[DB-INIT] Failed to create achievements: %v", err)
	}
	if _, err := db.Exec(SQL.CreateUserAchievementsTable); err != nil {
		logger.Errorf("[DB-INIT] Failed to create user_achievements: %v", err)
	}

	seedAchievements()
	InitContactsTable()
	InitTokenUsageTable()
	return nil
}

func seedAchievements() {
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM achievements").Scan(&count)
	//Why: Seeds initial achievement data if the current count is below established baseline thresholds.
	if count < 5 {
		//Why: Resets achievements to ensure the latest definitions are applied during the seeding process, while noting that 'INSERT OR REPLACE' is standard for production.
		_, _ = db.Exec("DELETE FROM achievements;")
		_, _ = db.Exec(`INSERT INTO achievements (name, description, icon, criteria_type, criteria_value, target_value, xp_reward) VALUES 
			('첫 걸음', '첫 번째 업무를 완료했습니다.', '🌱', 'total_tasks', 1, 1, 10),
			('모닝 스타', '오전 9시 이전에 첫 번째 업무를 완료했습니다.', '🌅', 'early_bird', 1, 1, 50),
			('불끄기 (Fire Extinguisher)', '긴급(Emergency) 태스크를 완료했습니다.', '🧯', 'emergency_tasks', 1, 1, 50),
			('Task Master', '하루 10개 이상의 작업 완료', '🏆', 'daily_total', 10, 10, 50),
			('스트릭 스타터', '3일 연속으로 업무를 완료했습니다.', '🔥', 'streak', 3, 3, 50),
			('끈기 끝판왕', '7일 연속으로 업무를 완료했습니다.', '👑', 'streak', 7, 7, 50),
			('태스크 마스터 I', '누적 10개의 업무를 완료했습니다.', '🏅', 'total_tasks', 10, 10, 50),
			('태스크 마스터 II', '누적 50개의 업무를 완료했습니다.', '🎖️', 'total_tasks', 50, 50, 100),
			('태스크 마스터 III', '누적 100개의 업무를 완료했습니다!', '🏆', 'total_tasks', 100, 100, 200),
			('꾸준함의 시작', '레벨 5에 도달했습니다.', '⭐', 'level', 5, 5, 100)`)
	}
}

func createIndexes() {
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_messages_task ON messages (task);",
		"CREATE INDEX IF NOT EXISTS idx_messages_room ON messages (room);",
		"CREATE INDEX IF NOT EXISTS idx_messages_requester ON messages (requester);",
		"CREATE INDEX IF NOT EXISTS idx_messages_assignee ON messages (assignee);",
		"CREATE INDEX IF NOT EXISTS idx_messages_original_text ON messages (original_text);",
		"CREATE INDEX IF NOT EXISTS idx_messages_source ON messages (source);",
		"CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages (created_at DESC);",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages (user_email);",
		"CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages (is_deleted);",
		"CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages (completed_at) WHERE done = 1;",
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_user_source_ts ON messages(user_email, source_ts);",
		"CREATE INDEX IF NOT EXISTS idx_task_translations_id_lang_code ON task_translations (language_code);",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_deleted_created ON messages (user_email, is_deleted, created_at DESC);",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_done_completed ON messages (user_email, done, completed_at DESC);",
	}
	for _, idx := range indexes {
		_, _ = db.Exec(idx)
	}
}

func GetDB() *sql.DB {
	return db
}

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
//Why: Enforces consistent transaction management across the gamification domain to ensure data integrity.
func RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func RefreshAllCaches() error {
	users, err := GetAllUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := RefreshCache(u.Email); err != nil {
			logger.Errorf("Failed to refresh cache for %s: %v", u.Email, err)
		}
	}
	return nil
}

func RefreshCache(email string) error {
	//Why: Prevents cache refresh operations from hanging indefinitely by enforcing a 10-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	safeArchiveDays := GetAutoArchiveDays()
	threshold := fmt.Sprintf("-%d days", safeArchiveDays)

	//Why: Retrieves recently active messages to populate the primary cache.
	rows, err := db.QueryContext(ctx, SQL.RefreshCacheActive, email, threshold)
	if err != nil {
		return err
	}
	defer rows.Close()

	var newActive = []ConsolidatedMessage{}
	newKnownTS := make(map[string]bool)
	for rows.Next() {
		m, err := scanMessageRow(rows)
		if err != nil {
			return err
		}
		newActive = append(newActive, m)
		newKnownTS[m.SourceTS] = true
	}

	//Why: Retrieves recently archived messages to populate the secondary cache.
	rowsArch, err := db.QueryContext(ctx, SQL.RefreshCacheArchive, email, threshold)
	if err != nil {
		return err
	}
	defer rowsArch.Close()

	var newArchive = []ConsolidatedMessage{}
	for rowsArch.Next() {
		m, err := scanMessageRow(rowsArch)
		if err != nil {
			return err
		}
		newArchive = append(newArchive, m)
		newKnownTS[m.SourceTS] = true
	}

	cacheMu.Lock()
	messageCache[email] = newActive
	archiveCache[email] = newArchive
	knownTS[email] = newKnownTS
	cacheInitialized[email] = true
	cacheMu.Unlock()

	return nil
}

func scanMessageRow(rows interface{ Scan(...interface{}) error }) (ConsolidatedMessage, error) {
	var m ConsolidatedMessage
	var assignedAt, createdAt, completedAt DBTime
	var room, requester, assignee, link, originalText, category, deadline, threadID, assigneeReason, repliedToID, sourceTS, source, requesterCanonical, assigneeCanonical sql.NullString

	err := rows.Scan(
		&m.ID, &m.UserEmail, &source, &room, &m.Task,
		&requester, &assignee, &assignedAt, &link,
		&sourceTS, &originalText, &m.Done, &m.IsDeleted,
		&createdAt, &completedAt, &category, &deadline,
		&threadID, &assigneeReason, &repliedToID, &requesterCanonical, &assigneeCanonical,
	)
	if err != nil {
		return m, err
	}

	m.Source = source.String
	m.Room = room.String
	m.Requester = requester.String
	m.Assignee = assignee.String
	m.Link = link.String
	m.SourceTS = sourceTS.String
	m.OriginalText = originalText.String
	m.Category = category.String
	m.Deadline = deadline.String
	m.ThreadID = threadID.String
	m.AssigneeReason = assigneeReason.String
	m.RepliedToID = repliedToID.String
	m.RequesterCanonical = requesterCanonical.String
	m.AssigneeCanonical = assigneeCanonical.String

	m.AssignedAt = assignedAt.Time
	m.CreatedAt = createdAt.Time
	if completedAt.Valid && !completedAt.Time.IsZero() {
		m.CompletedAt = &completedAt.Time
	}

	if m.AssignedAt.IsZero() && !m.CreatedAt.IsZero() {
		m.AssignedAt = m.CreatedAt
	}
	return m, nil
}

func EnsureCacheInitialized(email string) error {
	cacheMu.RLock()
	initialized := cacheInitialized[email]
	cacheMu.RUnlock()

	if !initialized {
		return RefreshCache(email)
	}
	return nil
}

func ArchiveOldTasks() error {
	archiveMu.Lock()
	defer archiveMu.Unlock()

	//Why: Throttles background archiving to once every six hours to optimize resource usage.
	if time.Since(lastArchiveTime) < 6*time.Hour {
		return nil
	}

	//Why: Limits archiving task duration to 15 seconds to prevent database performance degradation or locks.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	safeArchiveDays := GetAutoArchiveDays()
	threshold := fmt.Sprintf("-%d days", safeArchiveDays)

	logger.Infof("[DB] Auto-archiving tasks completed more than %d days ago...", safeArchiveDays)
	res, err := db.ExecContext(ctx, SQL.ArchiveOldTasks, threshold)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	logger.Infof("[DB] Auto-archived %d tasks.", rows)

	lastArchiveTime = time.Now()

	if rows > 0 {
		_ = RefreshAllCaches()
	}
	return nil
}

func LogDBStats() {
	if db == nil {
		return
	}
	stats := db.Stats()
	logger.Debugf("[DB-STATS] Open: %d | InUse: %d | Idle: %d | WaitCount: %d", stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
}
