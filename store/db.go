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

func InitDB(cfg *config.Config) error {
	var err error
	dbURL := cfg.TursoURL
	authToken := cfg.TursoToken

	// Remote-only Turso (libsql://)
	if strings.HasPrefix(dbURL, "libsql://") && authToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	}

	// Embedded Replicas (file: path with sync_url)
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

	if err := createCoreTables(); err != nil {
		return err
	}

	if err := runMigrations(); err != nil {
		return err
	}

	if err := setupGamification(); err != nil {
		return err
	}

	createIndexes()

	return RefreshAllCaches()
}

func setupConnectionPool(connStr string) {
	// Turso/libSQL은 Neon과 달리 서버리스 구조이므로 커넥션 풀을 유연하게 가져감.
	// 커넥션이 끊어지는 'stream is closed' 오류 방지를 위해 더 보수적으로 설정함.
	idleConns := 2
	if strings.HasPrefix(connStr, "libsql://") {
		// 원격 서버리스 환경에서는 유휴 커넥션 유지로 인한 bad connection 방지를 위해 0으로 설정
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
	_, err = db.Exec(SQL.CreateMessagesView)
	if err != nil {
		return err
	}
	_, err = db.Exec(SQL.CreateUsersView)
	return err
}

func runMigrations() error {
	// Column additions
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN user_email TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN is_deleted BOOLEAN DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN room TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN done BOOLEAN DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN completed_at DATETIME;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN original_text TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN category TEXT DEFAULT 'todo';")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN deadline TEXT;")

	// Users Gamification Columns
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN points INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN streak INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN level INTEGER DEFAULT 1;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN xp INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN daily_goal INTEGER DEFAULT 5;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN last_completed_at DATETIME;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN streak_freezes INTEGER DEFAULT 0;")

	if _, err := db.Exec("ALTER TABLE achievements ADD COLUMN target_value INTEGER DEFAULT 0;"); err != nil {
		logger.Errorf("[DB-MIGRATE] Error adding target_value: %v", err)
	}
	if _, err := db.Exec("ALTER TABLE achievements ADD COLUMN xp_reward INTEGER DEFAULT 0;"); err != nil {
		logger.Errorf("[DB-MIGRATE] Error adding xp_reward: %v", err)
	}

	migrateExistingData()
	return nil
}

func migrateExistingData() {
	// Ensure basic fields are not null
	_, _ = db.Exec("UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL;")
	_, _ = db.Exec("UPDATE messages SET room = '' WHERE room IS NULL;")
	_, _ = db.Exec("UPDATE messages SET category = 'waiting' WHERE task LIKE '[회신 대기]%';")
	_, _ = db.Exec("UPDATE messages SET category = 'promise' WHERE task LIKE '[나의 약속]%';")

	// Constraint migration (SQLite doesn't support DROP CONSTRAINT as easily)
	// We'll skip this if it was a clean migration to Turso.
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
	if count == 0 {
		_, _ = db.Exec(`INSERT INTO achievements (name, description, icon, criteria_type, criteria_value, xp_reward) VALUES 
			('첫 걸음', '첫 번째 업무를 완료했습니다.', '🌱', 'total_tasks', 1, 10),
			('태스크 마스터 I', '누적 10개의 업무를 완료했습니다.', '🏅', 'total_tasks', 10, 50),
			('태스크 마스터 II', '누적 50개의 업무를 완료했습니다.', '🎖️', 'total_tasks', 50, 100),
			('태스크 마스터 III', '누적 100개의 업무를 완료했습니다!', '🏆', 'total_tasks', 100, 200),
			('꾸준함의 시작', '레벨 5에 도달했습니다.', '⭐', 'level', 5, 100)`)
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
		"CREATE INDEX IF NOT EXISTS idx_task_translations_language ON task_translations (language);",
	}
	for _, idx := range indexes {
		_, _ = db.Exec(idx)
	}
}

func GetDB() *sql.DB {
	return db
}

// RunInTx executes a database transaction and automatically rolls it back if an error occurs.
// This wrapper enforces the "Transaction Management" concern and will be used
// extensively when Gamification logic becomes more complex.
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
	// 최대 10초 대기 후 강제 취소 (무한 Hang 원천 차단)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	safeArchiveDays := getArchiveDays()

	// 1. Fetch Active Messages
	rows, err := db.QueryContext(ctx, SQL.RefreshCacheActive, email, safeArchiveDays)
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

	// 2. Fetch Archived Messages (is_deleted = 1 OR long completed)
	rowsArch, err := db.QueryContext(ctx, SQL.RefreshCacheArchive, email, safeArchiveDays)
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
	err := rows.Scan(
		&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task,
		&m.Requester, &m.Assignee, &m.AssignedAt, &m.Link,
		&m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted,
		&m.CreatedAt, &m.CompletedAt, &m.Category, &m.Deadline,
	)
	return m, err
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

	// Rate-limit: Run at most once every 6 hours
	if time.Since(lastArchiveTime) < 6*time.Hour {
		return nil
	}

	// 백그라운드 아카이브 작업은 최대 15초 대기
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	safeArchiveDays := getArchiveDays()

	logger.Infof("[DB] Auto-archiving tasks completed more than %d days ago...", safeArchiveDays)
	res, err := db.ExecContext(ctx, SQL.ArchiveOldTasks, safeArchiveDays)
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
	logger.Infof("[DB-STATS] Open: %d | InUse: %d | Idle: %d | WaitCount: %d", stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount)
}
