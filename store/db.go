package store

import (
	"database/sql"
	"fmt"
	"message-consolidator/logger"
	"time"

	_ "github.com/lib/pq"
)

func InitDB(connStr string) error {
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Connection Pool Optimization for Neon (Scale to Zero)
	db.SetMaxIdleConns(0)  // 유휴 커넥션 즉시 종료 (Scale-to-Zero 비용 최적화)
	db.SetMaxOpenConns(20) // Cold Start 후 한 번에 몰리는 쿼리를 감당할 수 있도록 최대 연결 수 확장

	query := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		email TEXT UNIQUE,
		name TEXT,
		slack_id TEXT,
		wa_jid TEXT,
		picture TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS messages (
		id SERIAL PRIMARY KEY,
		user_email TEXT,
		source TEXT,
		room TEXT,
		task TEXT,
		requester TEXT,
		assignee TEXT,
		assigned_at TEXT,
		link TEXT,
		source_ts TEXT,
		original_text TEXT,
		done BOOLEAN DEFAULT false,
		is_deleted BOOLEAN DEFAULT false,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP
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
	);`

	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add user_email column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS user_email TEXT;")
	// Add is_deleted column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN DEFAULT false;")

	// Migration: Clean up duplicates before assigning existing data to jjsong@whatap.io
	// This prevents "duplicate key value violates unique constraint" when applying user_email
	_, err = db.Exec(`
		DELETE FROM messages 
		WHERE id NOT IN (
			SELECT MIN(id) 
			FROM messages 
			GROUP BY 
				CASE 
					WHEN user_email IS NULL OR user_email = '' THEN 'jjsong@whatap.io' 
					ELSE user_email 
				END, 
				source_ts
		);
	`)
	if err != nil {
		logger.Warnf("Migration cleanup error: %v", err)
	}

	// Migration: Assign existing data to jjsong@whatap.io
	_, err = db.Exec("UPDATE messages SET user_email = 'jjsong@whatap.io' WHERE user_email IS NULL OR user_email = '';")
	if err != nil {
		logger.Errorf("Migration error: %v", err)
	}
	// Ensure no NULLs in is_deleted
	_, _ = db.Exec("UPDATE messages SET is_deleted = false WHERE is_deleted IS NULL;")

	// Add room column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS room TEXT;")
	// Fill NULL rooms with empty strings
	_, _ = db.Exec("UPDATE messages SET room = '' WHERE room IS NULL;")
	// Add done column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS done INTEGER DEFAULT 0;")
	// Add completed_at column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS completed_at TIMESTAMP;")
	// Add original_text column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS original_text TEXT;")

	// Initialize Cache for all existing users
	if err := RefreshAllCaches(); err != nil {
		logger.Warnf("Failed to initial cache load: %v", err)
	}

	// Migration: Update UNIQUE constraint for multi-tenancy
	_, _ = db.Exec("ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_source_ts_key;")
	_, _ = db.Exec("ALTER TABLE messages ADD CONSTRAINT messages_user_ts_unique UNIQUE (user_email, source_ts);")

	// Index migrations for performance optimization
	_, _ = db.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm;")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_task_trgm ON messages USING gin (task gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_room_trgm ON messages USING gin (room gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_requester_trgm ON messages USING gin (requester gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_assignee_trgm ON messages USING gin (assignee gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_original_text_trgm ON messages USING gin (original_text gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages (created_at DESC);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages (user_email);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages (is_deleted);")

	// 완료된 업무의 빠른 조회 및 자동 아카이빙 성능을 위한 부분 인덱스
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_completed_at ON messages (completed_at) WHERE done = true;")

	// 1. 아카이브 검색 병목 해결: OR 조건 중 누락되었던 source 컬럼의 Trigram 인덱스 추가
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_source_trgm ON messages USING gin (source gin_trgm_ops);")

	// 2. 아카이브 정렬 최적화: CASE 구문 결과를 미리 색인해두는 복합 함수형 인덱스
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_archive_sort ON messages (user_email, (CASE WHEN is_deleted = true THEN created_at ELSE completed_at END) DESC);")

	// Create scan_metadata table for incremental scanning
	query = `CREATE TABLE IF NOT EXISTS scan_metadata (
		id SERIAL PRIMARY KEY,
		user_email TEXT NOT NULL,
		source TEXT NOT NULL,
		target_id TEXT NOT NULL,
		last_ts TEXT,
		UNIQUE(user_email, source, target_id)
	);`
	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create scan_metadata table: %w", err)
	}

	// Initialize New Tables
	InitContactsTable()
	InitTokenUsageTable()

	return nil
}

func GetDB() *sql.DB {
	return db
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
	// 1. Fetch Active Messages
	queryActive := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at 
		FROM messages 
		WHERE user_email = $1 AND is_deleted = false AND (done = false OR (done = true AND (completed_at IS NULL OR completed_at > NOW() - INTERVAL '%d days')))
		ORDER BY created_at DESC 
		LIMIT 200`, autoArchiveDays)
	rows, err := db.Query(queryActive, email)
	if err != nil {
		return err
	}
	defer rows.Close()

	var newActive = []ConsolidatedMessage{}
	newKnownTS := make(map[string]bool)
	for rows.Next() {
		var m ConsolidatedMessage
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt); err != nil {
			return err
		}
		newActive = append(newActive, m)
		newKnownTS[m.SourceTS] = true
	}

	// 2. Fetch Archived Messages (is_deleted = 1 OR long completed)
	queryArchive := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = true OR (done = true AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '%d days'))
		ORDER BY CASE WHEN is_deleted = true THEN created_at ELSE completed_at END DESC
		LIMIT 100`, autoArchiveDays)
	rowsArch, err := db.Query(queryArchive, email)
	if err != nil {
		return err
	}
	defer rowsArch.Close()

	var newArchive = []ConsolidatedMessage{}
	for rowsArch.Next() {
		var m ConsolidatedMessage
		if err := rowsArch.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &m.Done, &m.IsDeleted, &m.CreatedAt, &m.CompletedAt); err != nil {
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

	logger.Infof("[DB] Auto-archiving tasks completed more than %d days ago...", autoArchiveDays)
	query := fmt.Sprintf("UPDATE messages SET is_deleted = true WHERE is_deleted = false AND done = true AND completed_at < NOW() - INTERVAL '%d days'", autoArchiveDays)
	res, err := db.Exec(query)
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
