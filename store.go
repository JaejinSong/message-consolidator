package main

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// Types moved to types.go


var (
	db               *sql.DB
	messageCache     = make(map[string][]ConsolidatedMessage)
	archiveCache     = make(map[string][]ConsolidatedMessage)
	knownTS          = make(map[string]map[string]bool) // user_email -> source_ts -> bool
	cacheInitialized = make(map[string]bool)
	cacheMu          sync.RWMutex

	// Memory Caches for Metadata (to avoid DB hits during idle scans)
	userCache     = make(map[string]*User)
	aliasCache    = make(map[int][]string)
	scanCache     = make(map[string]string) // key: email:source:targetID -> lastTS
	dirtyScanKeys = make(map[string]bool)   // DB에 아직 persist 안 된 변경된 scanTS 목록
	tokenCache    = make(map[string]string) // email -> gmail token json
	metadataMu   sync.RWMutex
	lastArchiveTime time.Time
	archiveMu       sync.Mutex
)

func InitDB(connStr string) error {
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Connection Pool Optimization for Neon (Scale to Zero)
	db.SetConnMaxIdleTime(3 * time.Minute) // Close idle connections before Neon's 5-minute timeout
	db.SetMaxIdleConns(2)                  // Allow up to 2 idle connections for better performance
	db.SetMaxOpenConns(10)                 // Safety limit

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
		done INTEGER DEFAULT 0,
		is_deleted INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS task_translations (
		message_id INTEGER REFERENCES messages(id) ON DELETE CASCADE,
		language TEXT NOT NULL,
		translated_text TEXT NOT NULL,
		PRIMARY KEY (message_id, language)
	);`

	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add user_email column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS user_email TEXT;")
	// Add is_deleted column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_deleted INTEGER DEFAULT 0;")

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
		warnf("Migration cleanup error: %v", err)
	}

	// Migration: Assign existing data to jjsong@whatap.io
	_, err = db.Exec("UPDATE messages SET user_email = 'jjsong@whatap.io' WHERE user_email IS NULL OR user_email = '';")
	if err != nil {
		errorf("Migration error: %v", err)
	}
	// Ensure is_deleted is not null
	_, _ = db.Exec("UPDATE messages SET is_deleted = 0 WHERE is_deleted IS NULL;")

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
		warnf("Failed to initial cache load: %v", err)
	}

	// Migration: Update UNIQUE constraint for multi-tenancy
	_, _ = db.Exec("ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_source_ts_key;")
	_, _ = db.Exec("ALTER TABLE messages ADD CONSTRAINT messages_user_ts_unique UNIQUE (user_email, source_ts);")

	// Index migrations for performance optimization
	_, _ = db.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm;")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_task_trgm ON messages USING gin (task gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_room_trgm ON messages USING gin (room gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_requester_trgm ON messages USING gin (requester gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_original_text_trgm ON messages USING gin (original_text gin_trgm_ops);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_created_at_desc ON messages (created_at DESC);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_user_email ON messages (user_email);")
	_, _ = db.Exec("CREATE INDEX IF NOT EXISTS idx_messages_is_deleted ON messages (is_deleted);")

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

	return nil
}

func LoadMetadata() error {
	metadataMu.Lock()
	defer metadataMu.Unlock()

	infof("[CACHE] Initializing metadata cache from DB...")

	// 1. Load Users
	rows, err := db.Query("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at FROM users")
	if err != nil {
		return fmt.Errorf("failed to load users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt); err != nil {
			return err
		}
		userCache[u.Email] = &u
	}

	// 2. Load User Aliases
	aliasRows, err := db.Query("SELECT user_id, alias_name FROM user_aliases")
	if err != nil {
		return fmt.Errorf("failed to load user aliases: %w", err)
	}
	defer aliasRows.Close()

	for aliasRows.Next() {
		var userID int
		var alias string
		if err := aliasRows.Scan(&userID, &alias); err != nil {
			continue
		}
		aliasCache[userID] = append(aliasCache[userID], alias)
	}

	// 3. Load Scan Metadata
	scanRows, err := db.Query("SELECT user_email, source, target_id, last_ts FROM scan_metadata")
	if err != nil {
		return fmt.Errorf("failed to load scan metadata: %w", err)
	}
	defer scanRows.Close()

	for scanRows.Next() {
		var email, source, targetID, lastTS string
		if err := scanRows.Scan(&email, &source, &targetID, &lastTS); err != nil {
			continue
		}
		key := fmt.Sprintf("%s:%s:%s", email, source, targetID)
		scanCache[key] = lastTS
	}

	// 4. Load Gmail Tokens
	tokenRows, err := db.Query("SELECT user_email, token_json FROM gmail_tokens")
	if err != nil {
		return fmt.Errorf("failed to load gmail tokens: %w", err)
	}
	defer tokenRows.Close()

	for tokenRows.Next() {
		var email, tokenJSON string
		if err := tokenRows.Scan(&email, &tokenJSON); err != nil {
			continue
		}
		tokenCache[email] = tokenJSON
	}

	infof("[CACHE] Loaded %d users, %d scan metadata entries, %d tokens.", len(userCache), len(scanCache), len(tokenCache))
	return nil
}

func GetLastScan(userEmail, source, targetID string) string {
	metadataMu.RLock()
	defer metadataMu.RUnlock()
	key := fmt.Sprintf("%s:%s:%s", userEmail, source, targetID)
	return scanCache[key]
}

func UpdateLastScan(userEmail, source, targetID, ts string) error {
	metadataMu.Lock()
	key := fmt.Sprintf("%s:%s:%s", userEmail, source, targetID)
	oldTS := scanCache[key]
	scanCache[key] = ts
	// 실제로 값이 변경된 경우에만 dirty 마킹 → PersistAllScanMetadata가 DB에 씁니다
	if ts != oldTS {
		dirtyScanKeys[key] = true
	}
	metadataMu.Unlock()

	debugf("[CACHE] Updated memory scan_ts for %s:%s -> %s (dirty: %v)", source, targetID, ts, ts != oldTS)
	return nil
}

func PersistScanMetadata(userEmail, source, targetID, ts string) error {
	query := `INSERT INTO scan_metadata (user_email, source, target_id, last_ts)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_email, source, target_id)
		DO UPDATE SET last_ts = EXCLUDED.last_ts;`
	_, err := db.Exec(query, userEmail, source, targetID, ts)
	return err
}

func PersistAllScanMetadata(userEmail string) {
	metadataMu.RLock()
	var toPersist []struct{ source, target, ts string }
	prefix := userEmail + ":"
	for key, ts := range scanCache {
		// dirty 상태인 항목만 DB에 persist (NeonDB Sleep 최적화)
		if strings.HasPrefix(key, prefix) && dirtyScanKeys[key] {
			parts := strings.Split(key, ":")
			if len(parts) == 3 {
				toPersist = append(toPersist, struct{ source, target, ts string }{parts[1], parts[2], ts})
			}
		}
	}
	metadataMu.RUnlock()

	for _, item := range toPersist {
		_ = PersistScanMetadata(userEmail, item.source, item.target, item.ts)
		// persist 완료 후 dirty 플래그 해제
		metadataMu.Lock()
		delete(dirtyScanKeys, userEmail+":"+item.source+":"+item.target)
		metadataMu.Unlock()
	}
}

func GetAllUsers() ([]User, error) {
	metadataMu.RLock()
	defer metadataMu.RUnlock()

	var users []User
	for _, u := range userCache {
		users = append(users, *u)
	}
	return users, nil
}

func GetOrCreateUser(email, name, picture string) (*User, error) {
	metadataMu.Lock()
	if u, ok := userCache[email]; ok {
		metadataMu.Unlock()
		return u, nil
	}
	metadataMu.Unlock()

	// Not in cache, fetch from DB or Create
	var u User
	err := db.QueryRow("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at FROM users WHERE email = $1", email).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt)
	if err == sql.ErrNoRows {
		err = db.QueryRow("INSERT INTO users (email, name, picture) VALUES ($1, $2, $3) RETURNING id, email, name, COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at", email, name, picture).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt)
		if err != nil {
			return nil, err
		}
		metadataMu.Lock()
		userCache[email] = &u
		metadataMu.Unlock()
		return &u, nil
	}
	if err != nil {
		return nil, err
	}

	metadataMu.Lock()
	userCache[email] = &u
	metadataMu.Unlock()

	return &u, nil
}

func UpdateUserWAJID(email, wajid string) error {
	_, err := db.Exec("UPDATE users SET wa_jid = $1 WHERE email = $2", wajid, email)
	return err
}

func RefreshAllCaches() error {
	users, err := GetAllUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := RefreshCache(u.Email); err != nil {
			errorf("Failed to refresh cache for %s: %v", u.Email, err)
		}
	}
	return nil
}

func RefreshCache(email string) error {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	// 1. Fetch Active Messages
	queryActive := `
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at 
		FROM messages 
		WHERE user_email = $1 AND is_deleted = 0 AND (done = 0 OR (done = 1 AND (completed_at IS NULL OR completed_at > NOW() - INTERVAL '6 days')))
		ORDER BY created_at DESC 
		LIMIT 200`
	rows, err := db.Query(queryActive, email)
	if err != nil {
		return err
	}
	defer rows.Close()

	var newActive = []ConsolidatedMessage{}
	newKnownTS := make(map[string]bool)
	for rows.Next() {
		var m ConsolidatedMessage
		var doneInt, delInt int
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &doneInt, &delInt, &m.CreatedAt, &m.CompletedAt); err != nil {
			return err
		}
		m.Done = doneInt == 1
		m.IsDeleted = delInt == 1
		newActive = append(newActive, m)
		newKnownTS[m.SourceTS] = true
	}
	messageCache[email] = newActive

	// 2. Fetch Archived Messages (is_deleted = 1 OR long completed)
	queryArchive := `
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '6 days'))
		ORDER BY CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC
		LIMIT 100`
	rowsArch, err := db.Query(queryArchive, email)
	if err != nil {
		return err
	}
	defer rowsArch.Close()

	var newArchive = []ConsolidatedMessage{}
	for rowsArch.Next() {
		var m ConsolidatedMessage
		var doneInt, delInt int
		if err := rowsArch.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &doneInt, &delInt, &m.CreatedAt, &m.CompletedAt); err != nil {
			return err
		}
		m.Done = doneInt == 1
		m.IsDeleted = delInt == 1
		newArchive = append(newArchive, m)
		newKnownTS[m.SourceTS] = true
	}
	archiveCache[email] = newArchive
	knownTS[email] = newKnownTS
	cacheInitialized[email] = true

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

func SaveMessage(msg ConsolidatedMessage) (bool, error) {
	cacheMu.RLock()
	if userKnown, ok := knownTS[msg.UserEmail]; ok && userKnown[msg.SourceTS] {
		cacheMu.RUnlock()
		return false, nil
	}
	cacheMu.RUnlock()

	query := `INSERT INTO messages (user_email, source, room, task, requester, assignee, assigned_at, link, source_ts, original_text) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			  ON CONFLICT(user_email, source_ts) DO NOTHING;`
	res, err := db.Exec(query, msg.UserEmail, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS, msg.OriginalText)
	if err != nil {
		errorf("SaveMessage Error: %v", err)
		return false, err
	}

	rows, _ := res.RowsAffected()
	saved := rows > 0

	if saved {
		cacheMu.Lock()
		if _, ok := knownTS[msg.UserEmail]; !ok {
			knownTS[msg.UserEmail] = make(map[string]bool)
		}
		knownTS[msg.UserEmail][msg.SourceTS] = true
		cacheMu.Unlock()
	}

	return saved, nil
}

func GetMessages(email string) ([]ConsolidatedMessage, error) {
	if err := EnsureCacheInitialized(email); err != nil {
		errorf("Failed to ensure cache initialized for %s in GetMessages: %v", email, err)
	}

	cacheMu.RLock()
	msgs := messageCache[email]
	cacheMu.RUnlock()

	if msgs == nil {
		return []ConsolidatedMessage{}, nil
	}
	return msgs, nil
}

func MarkMessageDone(email string, id int, done bool) error {
	val := 0
	var completeTime interface{} = nil
	if done {
		val = 1
		completeTime = time.Now()
	}
	_, err := db.Exec("UPDATE messages SET done = $1, completed_at = $2 WHERE id = $3 AND user_email = $4", val, completeTime, id, email)
	if err == nil {
		go RefreshCache(email)
	}
	return err
}

func GetArchivedMessages(email string) ([]ConsolidatedMessage, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if msgs, ok := archiveCache[email]; ok {
		return msgs, nil
	}
	return []ConsolidatedMessage{}, nil
}

func GetArchivedMessagesFiltered(email string, limit, offset int, search string, sortField, sortOrder string) ([]ConsolidatedMessage, int, error) {
	searchQuery := ""
	args := []interface{}{email}
	argIdx := 2

	if search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		searchQuery = fmt.Sprintf(` AND (
			LOWER(task) ILIKE $%d OR 
			LOWER(room) ILIKE $%d OR 
			LOWER(requester) ILIKE $%d OR 
			LOWER(original_text) ILIKE $%d OR
			LOWER(source) ILIKE $%d
		)`, argIdx, argIdx, argIdx, argIdx, argIdx)
		args = append(args, pattern)
		argIdx++
	}

	// 1. Get Count
	countQuery := fmt.Sprintf(`
		SELECT COUNT(*) 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '6 days'))
		%s`, searchQuery)
	
	var total int
	err := db.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// 2. Get Data
	if limit <= 0 {
		limit = 100
	}
	
	// Default sorting
	orderBy := "CASE WHEN is_deleted = 1 THEN created_at ELSE completed_at END DESC"
	whitelist := map[string]string{
		"source":       "source",
		"room":         "room",
		"task":         "task",
		"requester":    "requester",
		"assignee":     "assignee",
		"created_at":   "created_at",
		"completed_at": "completed_at",
		"time":         "created_at",
	}

	if sortField != "" {
		if dbField, ok := whitelist[sortField]; ok {
			order := "ASC"
			if strings.ToUpper(sortOrder) == "DESC" {
				order = "DESC"
			}
			orderBy = fmt.Sprintf("%s %s", dbField, order)
		}
	}

	dataQuery := fmt.Sprintf(`
		SELECT id, user_email, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, COALESCE(original_text, ''), done, is_deleted, created_at, completed_at 
		FROM messages 
		WHERE user_email = $1 AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '6 days'))
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, searchQuery, orderBy, argIdx, argIdx+1)
	
	args = append(args, limit, offset)
	
	rows, err := db.Query(dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		var m ConsolidatedMessage
		var doneInt, delInt int
		if err := rows.Scan(&m.ID, &m.UserEmail, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &m.OriginalText, &doneInt, &delInt, &m.CreatedAt, &m.CompletedAt); err != nil {
			return nil, 0, err
		}
		m.Done = doneInt == 1
		m.IsDeleted = delInt == 1
		msgs = append(msgs, m)
	}

	return msgs, total, nil
}

func UpdateTaskText(email string, id int, task string) error {
	_, err := db.Exec("UPDATE messages SET task = $1 WHERE id = $2 AND user_email = $3", task, id, email)
	if err == nil {
		go RefreshCache(email)
	}
	return err
}

func ArchiveOldTasks() error {
	archiveMu.Lock()
	defer archiveMu.Unlock()

	// Rate-limit: Run at most once every 6 hours
	if time.Since(lastArchiveTime) < 6*time.Hour {
		return nil
	}

	infof("[DB] Auto-archiving tasks completed more than 6 days ago...")
	res, err := db.Exec("UPDATE messages SET is_deleted = 1 WHERE is_deleted = 0 AND done = 1 AND completed_at < NOW() - INTERVAL '6 days'")
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	infof("[DB] Auto-archived %d tasks.", rows)
	
	lastArchiveTime = time.Now()

	if rows > 0 {
		_ = RefreshAllCaches()
	}
	return nil
}

func DeleteMessage(email string, id int) error {
	res, err := db.Exec("UPDATE messages SET is_deleted = 1 WHERE id = $1 AND user_email = $2", id, email)
	if err == nil {
		rows, _ := res.RowsAffected()
		debugf("[DB] Soft-delete message ID %d, affected rows: %d", id, rows)
		go RefreshCache(email)
	}
	return err
}

func HardDeleteMessage(email string, id int) error {
	res, err := db.Exec("DELETE FROM messages WHERE id = $1 AND user_email = $2", id, email)
	if err == nil {
		rows, _ := res.RowsAffected()
		debugf("[DB] Hard-delete message ID %d, affected rows: %d", id, rows)
		go RefreshCache(email)
	}
	return err
}

func RestoreMessage(email string, id int) error {
	res, err := db.Exec("UPDATE messages SET is_deleted = 0 WHERE id = $1 AND user_email = $2", id, email)
	if err == nil {
		rows, _ := res.RowsAffected()
		debugf("[DB] Restore message ID %d, affected rows: %d", id, rows)
		go RefreshCache(email)
	}
	return err
}

func UpdateUserSlackID(email, slackID string) error {
	_, err := db.Exec("UPDATE users SET slack_id = $1 WHERE email = $2", slackID, email)
	return err
}

func GetUserAliases(userID int) ([]string, error) {
	metadataMu.RLock()
	aliases, ok := aliasCache[userID]
	metadataMu.RUnlock()
	if ok {
		return aliases, nil
	}

	rows, err := db.Query("SELECT alias_name FROM user_aliases WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var newAliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			continue
		}
		newAliases = append(newAliases, alias)
	}

	metadataMu.Lock()
	aliasCache[userID] = newAliases
	metadataMu.Unlock()

	return newAliases, nil
}

func AddUserAlias(userID int, alias string) error {
	if alias == "" {
		return nil
	}
	_, err := db.Exec("INSERT INTO user_aliases (user_id, alias_name) VALUES ($1, $2) ON CONFLICT (user_id, alias_name) DO NOTHING", userID, alias)
	return err
}

func DeleteUserAlias(userID int, alias string) error {
	_, err := db.Exec("DELETE FROM user_aliases WHERE user_id = $1 AND alias_name = $2", userID, alias)
	return err
}

func SaveGmailToken(email, tokenJSON string) error {
	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	_, err := db.Exec(`
		INSERT INTO gmail_tokens (user_email, token_json, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_email) DO UPDATE SET token_json = $2, updated_at = NOW()`,
		email, tokenJSON)
	return err
}

func GetGmailToken(email string) (string, error) {
	metadataMu.RLock()
	token, ok := tokenCache[email]
	metadataMu.RUnlock()
	if ok {
		return token, nil
	}

	var tokenJSON string
	err := db.QueryRow("SELECT token_json FROM gmail_tokens WHERE user_email = $1", email).Scan(&tokenJSON)
	if err != nil {
		return "", err
	}

	metadataMu.Lock()
	tokenCache[email] = tokenJSON
	metadataMu.Unlock()

	return tokenJSON, nil
}

func HasGmailToken(email string) bool {
	metadataMu.RLock()
	_, ok := tokenCache[email]
	metadataMu.RUnlock()
	return ok
}

// Translation Caching Functions

func GetTaskTranslation(messageID int, language string) (string, error) {
	var translatedText string
	err := db.QueryRow("SELECT translated_text FROM task_translations WHERE message_id = $1 AND language = $2", messageID, language).Scan(&translatedText)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return translatedText, err
}

func SaveTaskTranslation(messageID int, language, translatedText string) error {
	_, err := db.Exec(`
		INSERT INTO task_translations (message_id, language, translated_text)
		VALUES ($1, $2, $3)
		ON CONFLICT (message_id, language) DO UPDATE SET translated_text = EXCLUDED.translated_text`,
		messageID, language, translatedText)
	return err
}
