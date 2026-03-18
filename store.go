package main

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

type ConsolidatedMessage struct {
	ID          int        `json:"id"`
	UserEmail   string     `json:"user_email"`
	Source      string     `json:"source"` // slack or whatsapp
	Room        string     `json:"room"`
	Task        string     `json:"task"`
	Requester   string     `json:"requester"`
	Assignee    string     `json:"assignee"`
	AssignedAt  string     `json:"assigned_at"`
	Link        string     `json:"link"`
	SourceTS    string     `json:"source_ts"`
	OriginalText string     `json:"original_text"`
	Done        bool       `json:"done"`
	IsDeleted   bool       `json:"is_deleted"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type User struct {
	ID        int       `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	SlackID   string    `json:"slack_id"`
	WAJID     string    `json:"wa_jid"`
	Picture   string    `json:"picture"`
	Aliases   []string  `json:"aliases"`
	CreatedAt time.Time `json:"created_at"`
}

type UserAlias struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	AliasName string `json:"alias_name"`
}

var (
	db           *sql.DB
	messageCache = make(map[string][]ConsolidatedMessage)
	archiveCache = make(map[string][]ConsolidatedMessage)
	knownTS      = make(map[string]map[string]bool) // user_email -> source_ts -> bool
	cacheMu      sync.RWMutex
)

func InitDB(connStr string) error {
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Connection Pool Optimization for Neon (Scale to Zero)
	db.SetConnMaxIdleTime(3 * time.Minute) // Close idle connections before Neon's 5-minute timeout
	db.SetMaxIdleConns(2)                  // Allow limited reuse for efficiency
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
	);`

	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add user_email column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS user_email TEXT;")
	// Add is_deleted column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_deleted INTEGER DEFAULT 0;")
	
	// Migration: Assign existing data to jjsong@whatap.io
	_, err = db.Exec("UPDATE messages SET user_email = 'jjsong@whatap.io' WHERE user_email IS NULL OR user_email = '';")
	if err != nil {
		log.Printf("Migration error: %v", err)
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
		log.Printf("Warning: Failed to initial cache load: %v", err)
	}

	// Migration: Update UNIQUE constraint for multi-tenancy
	_, _ = db.Exec("ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_source_ts_key;")
	_, _ = db.Exec("ALTER TABLE messages ADD CONSTRAINT messages_user_ts_unique UNIQUE (user_email, source_ts);")

	return nil
}

func GetAllUsers() ([]User, error) {
	rows, err := db.Query("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func GetOrCreateUser(email, name, picture string) (*User, error) {
	var u User
	err := db.QueryRow("SELECT id, email, COALESCE(name, ''), COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at FROM users WHERE email = $1", email).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt)
	if err == sql.ErrNoRows {
		err = db.QueryRow("INSERT INTO users (email, name, picture) VALUES ($1, $2, $3) RETURNING id, email, name, COALESCE(slack_id, ''), COALESCE(wa_jid, ''), COALESCE(picture, ''), created_at", email, name, picture).Scan(&u.ID, &u.Email, &u.Name, &u.SlackID, &u.WAJID, &u.Picture, &u.CreatedAt)
		if err != nil {
			return nil, err
		}
		return &u, nil
	}
	if err != nil {
		return nil, err
	}

	// Update name or picture if changed
	if (name != "" && u.Name != name) || (picture != "" && u.Picture != picture) {
		_, _ = db.Exec("UPDATE users SET name = $1, picture = $2 WHERE email = $3", name, picture, email)
		u.Name = name
		u.Picture = picture
	}

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
			log.Printf("Failed to refresh cache for %s: %v", u.Email, err)
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
		WHERE user_email = $1 AND is_deleted = 0 AND (done = 0 OR (done = 1 AND (completed_at IS NULL OR completed_at > NOW() - INTERVAL '7 days')))
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
		WHERE user_email = $1 AND (is_deleted = 1 OR (done = 1 AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '7 days'))
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
		log.Printf("SaveMessage Error: %v", err)
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
	cacheMu.RLock()
	msgs, ok := messageCache[email]
	cacheMu.RUnlock()

	if !ok || len(msgs) == 0 {
		// Try refreshing cache if empty
		if err := RefreshCache(email); err != nil {
			log.Printf("Failed to refresh cache for %s inside GetMessages: %v", email, err)
		}
		cacheMu.RLock()
		msgs = messageCache[email]
		cacheMu.RUnlock()
	}
	
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

func UpdateTaskText(email string, id int, task string) error {
	_, err := db.Exec("UPDATE messages SET task = $1 WHERE id = $2 AND user_email = $3", task, id, email)
	if err == nil {
		go RefreshCache(email)
	}
	return err
}

func DeleteMessage(email string, id int) error {
	_, err := db.Exec("UPDATE messages SET is_deleted = 1 WHERE id = $1 AND user_email = $2", id, email)
	if err == nil {
		go RefreshCache(email)
	}
	return err
}

func UpdateUserSlackID(email, slackID string) error {
	_, err := db.Exec("UPDATE users SET slack_id = $1 WHERE email = $2", slackID, email)
	return err
}

func GetUserAliases(userID int) ([]string, error) {
	rows, err := db.Query("SELECT alias_name FROM user_aliases WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			continue
		}
		aliases = append(aliases, alias)
	}
	return aliases, nil
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
	_, err := db.Exec(`
		INSERT INTO gmail_tokens (user_email, token_json, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (user_email) DO UPDATE SET token_json = $2, updated_at = NOW()`,
		email, tokenJSON)
	return err
}

func GetGmailToken(email string) (string, error) {
	var tokenJSON string
	err := db.QueryRow("SELECT token_json FROM gmail_tokens WHERE user_email = $1", email).Scan(&tokenJSON)
	if err != nil {
		return "", err
	}
	return tokenJSON, nil
}

func HasGmailToken(email string) bool {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM gmail_tokens WHERE user_email = $1", email).Scan(&count)
	return count > 0
}
