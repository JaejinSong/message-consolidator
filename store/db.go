package store

import (
	"context"
	"database/sql"
	"fmt"
	"message-consolidator/logger"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func InitDB(connStr string) error {
	var err error
	db, err = sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	setupConnectionPool(connStr)

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
	if strings.Contains(connStr, ".neon.tech") {
		// Neon Pooler (PG 통계/스케일링 제약) 환경을 고려하여 0으로 설정
		logger.Infof("[DB] NeonDB detected. Setting MaxIdleConns to 0 for scale-to-zero.")
		db.SetMaxIdleConns(0)
	} else {
		logger.Infof("[DB] Standard DB detected. Setting MaxIdleConns to 2.")
		db.SetMaxIdleConns(2)
	}
	db.SetMaxOpenConns(20)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)
}

func createCoreTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		email TEXT UNIQUE,
		name TEXT,
		slack_id TEXT,
		wa_jid TEXT,
		picture TEXT,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS user_aliases (
		id SERIAL PRIMARY KEY,
		user_id INTEGER REFERENCES users(id),
		alias_name TEXT,
		UNIQUE(user_id, alias_name)
	);
	CREATE TABLE IF NOT EXISTS gmail_tokens (
		user_email TEXT PRIMARY KEY,
		token_json TEXT NOT NULL,
		updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS messages (
		id SERIAL PRIMARY KEY,
		user_email TEXT,
		source TEXT,
		room TEXT,
		task TEXT,
		requester TEXT,
		assignee TEXT,
		assigned_at TIMESTAMPTZ,
		link TEXT,
		source_ts TEXT,
		original_text TEXT,
		done BOOLEAN DEFAULT false,
		is_deleted BOOLEAN DEFAULT false,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMPTZ,
		category TEXT DEFAULT 'todo'
	);
	CREATE TABLE IF NOT EXISTS task_translations (
		message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
		language TEXT NOT NULL,
		translated_text TEXT NOT NULL,
		PRIMARY KEY (message_id, language)
	);
	CREATE TABLE IF NOT EXISTS tenant_aliases (
		id SERIAL PRIMARY KEY,
		user_email TEXT NOT NULL,
		original_name TEXT NOT NULL,
		primary_name TEXT NOT NULL,
		UNIQUE(user_email, original_name)
	);
	CREATE TABLE IF NOT EXISTS scan_metadata (
		id SERIAL PRIMARY KEY,
		user_email TEXT NOT NULL,
		source TEXT NOT NULL,
		target_id TEXT NOT NULL,
		last_ts TEXT,
		UNIQUE(user_email, source, target_id)
	);`

	_, err := db.Exec(query)
	return err
}

func runMigrations() error {
	// Column additions
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS user_email TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN DEFAULT false;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS room TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS done BOOLEAN DEFAULT false;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS original_text TEXT;")
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS category TEXT DEFAULT 'todo';")

	// Users Gamification Columns
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS points INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS streak INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS level INTEGER DEFAULT 1;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS xp INTEGER DEFAULT 0;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_goal INTEGER DEFAULT 5;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS last_completed_at TIMESTAMPTZ;")
	_, _ = db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS streak_freezes INTEGER DEFAULT 0;")

	if _, err := db.Exec("ALTER TABLE achievements ADD COLUMN IF NOT EXISTS target_value INTEGER DEFAULT 0;"); err != nil {
		logger.Errorf("[DB-MIGRATE] Error adding target_value: %v", err)
	}
	if _, err := db.Exec("ALTER TABLE achievements ADD COLUMN IF NOT EXISTS xp_reward INTEGER DEFAULT 0;"); err != nil {
		logger.Errorf("[DB-MIGRATE] Error adding xp_reward: %v", err)
	}

	// Migrate existing TIMESTAMP columns to TIMESTAMPTZ safely
	_, _ = db.Exec(`
		ALTER TABLE messages ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
		ALTER TABLE messages ALTER COLUMN completed_at TYPE TIMESTAMPTZ USING completed_at AT TIME ZONE 'UTC';
		ALTER TABLE users ALTER COLUMN created_at TYPE TIMESTAMPTZ USING created_at AT TIME ZONE 'UTC';
		ALTER TABLE users ALTER COLUMN last_completed_at TYPE TIMESTAMPTZ USING last_completed_at AT TIME ZONE 'UTC';
		ALTER TABLE gmail_tokens ALTER COLUMN updated_at TYPE TIMESTAMPTZ USING updated_at AT TIME ZONE 'UTC';
		ALTER TABLE user_achievements ALTER COLUMN unlocked_at TYPE TIMESTAMPTZ USING unlocked_at AT TIME ZONE 'UTC';
	`)

	migrateExistingData()
	return nil
}

func migrateExistingData() {
	// Ensure basic fields are not null
	_, _ = db.Exec("UPDATE messages SET is_deleted = false WHERE is_deleted IS NULL;")
	_, _ = db.Exec("UPDATE messages SET room = '' WHERE room IS NULL;")
	_, _ = db.Exec("UPDATE messages SET category = 'waiting' WHERE task LIKE '[회신 대기]%';")
	_, _ = db.Exec("UPDATE messages SET category = 'promise' WHERE task LIKE '[나의 약속]%';")

	// Constraint migration
	_, _ = db.Exec("ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_source_ts_key;")
	_, _ = db.Exec("ALTER TABLE messages ADD CONSTRAINT messages_user_ts_unique UNIQUE (user_email, source_ts);")
}

func setupGamification() error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS achievements (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		icon TEXT,
		criteria_type TEXT,
		criteria_value INTEGER,
		target_value INTEGER DEFAULT 0,
		xp_reward INTEGER DEFAULT 0
	);`); err != nil {
		logger.Errorf("[DB-INIT] Failed to create achievements: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS user_achievements (
		user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
		achievement_id INTEGER REFERENCES achievements(id) ON DELETE CASCADE,
		unlocked_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (user_id, achievement_id)
	);`); err != nil {
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
	_, _ = db.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm;")
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_messages_task_trgm ON messages USING gin (task gin_trgm_ops);",
		"CREATE INDEX IF NOT EXISTS idx_messages_room_trgm ON messages USING gin (room gin_trgm_ops);",
		"CREATE INDEX IF NOT EXISTS idx_messages_requester_trgm ON messages USING gin (requester gin_trgm_ops);",
		"CREATE INDEX IF NOT EXISTS idx_messages_assignee_trgm ON messages USING gin (assignee gin_trgm_ops);",
		"CREATE INDEX IF NOT EXISTS idx_messages_original_text_trgm ON messages USING gin (original_text gin_trgm_ops);",
		"CREATE INDEX IF NOT EXISTS idx_messages_source_trgm ON messages USING gin (source gin_trgm_ops);",
		"CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages (created_at DESC);",
		"CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages (user_email);",
		"CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages (is_deleted);",
		"CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages (completed_at) WHERE done = true;",
		"CREATE INDEX IF NOT EXISTS idx_messages_archive_sort ON messages (user_email, (CASE WHEN is_deleted = true THEN created_at ELSE completed_at END) DESC);",
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
	queryActive := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at, COALESCE(category, 'todo'), COALESCE(deadline, '') 
		FROM messages 
		WHERE user_email = $1 AND is_deleted = false AND (done = false OR (done = true AND (completed_at IS NULL OR completed_at > NOW() - INTERVAL '%d days')))
		ORDER BY created_at DESC 
		LIMIT 200`, safeArchiveDays)
	rows, err := db.QueryContext(ctx, queryActive, email)
	if err != nil {
		return err
	}
	defer rows.Close()

	var newActive = []ConsolidatedMessage{}
	newKnownTS := make(map[string]bool)
	for rows.Next() {
		var m ConsolidatedMessage
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt, &m.Category, &m.Deadline); err != nil {
			return err
		}
		newActive = append(newActive, m)
		newKnownTS[m.SourceTS] = true
	}

	// 2. Fetch Archived Messages (is_deleted = 1 OR long completed)
	queryArchive := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at, COALESCE(category, 'todo'), COALESCE(deadline, '') 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = true OR (done = true AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '%d days'))
		ORDER BY CASE WHEN is_deleted = true THEN created_at ELSE completed_at END DESC
		LIMIT 100`, safeArchiveDays)
	rowsArch, err := db.QueryContext(ctx, queryArchive, email)
	if err != nil {
		return err
	}
	defer rowsArch.Close()

	var newArchive = []ConsolidatedMessage{}
	for rowsArch.Next() {
		var m ConsolidatedMessage
		if err := rowsArch.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt, &m.Category, &m.Deadline); err != nil {
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
	query := fmt.Sprintf("UPDATE messages SET is_deleted = true WHERE is_deleted = false AND done = true AND completed_at < NOW() - INTERVAL '%d days'", safeArchiveDays)
	res, err := db.ExecContext(ctx, query)
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
