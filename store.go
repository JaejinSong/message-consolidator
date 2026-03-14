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
	Source      string     `json:"source"` // slack or whatsapp
	Room        string     `json:"room"`
	Task        string     `json:"task"`
	Requester   string     `json:"requester"`
	Assignee    string     `json:"assignee"`
	AssignedAt  string     `json:"assigned_at"`
	Link        string     `json:"link"`
	SourceTS    string     `json:"source_ts"`
	Done        bool       `json:"done"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

var (
	db           *sql.DB
	messageCache []ConsolidatedMessage
	archiveCache []ConsolidatedMessage
	knownTS      map[string]bool
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
	CREATE TABLE IF NOT EXISTS messages (
		id SERIAL PRIMARY KEY,
		source TEXT,
		room TEXT,
		task TEXT,
		requester TEXT,
		assignee TEXT,
		assigned_at TEXT,
		link TEXT,
		source_ts TEXT UNIQUE,
		done INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP
	);`

	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Add room column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS room TEXT;")
	// Fill NULL rooms with empty strings
	_, _ = db.Exec("UPDATE messages SET room = '' WHERE room IS NULL;")
	// Add done column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS done INTEGER DEFAULT 0;")
	// Add completed_at column if it doesn't exist
	_, _ = db.Exec("ALTER TABLE messages ADD COLUMN IF NOT EXISTS completed_at TIMESTAMP;")

	// Initialize Cache
	knownTS = make(map[string]bool)
	if err := RefreshCache(); err != nil {
		log.Printf("Warning: Failed to initial cache load: %v", err)
	}

	log.Println("Database initialized with Neon and In-Memory Cache")
	return nil
}

func RefreshCache() error {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	// 1. Fetch Active Messages
	queryActive := `
		SELECT id, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, done, created_at, completed_at 
		FROM messages 
		WHERE done = 0 OR (done = 1 AND (completed_at IS NULL OR completed_at > NOW() - INTERVAL '7 days'))
		ORDER BY created_at DESC 
		LIMIT 200`
	rows, err := db.Query(queryActive)
	if err != nil {
		return err
	}
	defer rows.Close()

	var newActive []ConsolidatedMessage
	newKnownTS := make(map[string]bool)
	for rows.Next() {
		var m ConsolidatedMessage
		var doneInt int
		if err := rows.Scan(&m.ID, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &doneInt, &m.CreatedAt, &m.CompletedAt); err != nil {
			return err
		}
		m.Done = doneInt == 1
		newActive = append(newActive, m)
		newKnownTS[m.SourceTS] = true
	}
	messageCache = newActive

	// 2. Fetch Archived Messages (Partial load for efficiency)
	queryArchive := `
		SELECT id, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, done, created_at, completed_at 
		FROM messages 
		WHERE done = 1 AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '7 days'
		ORDER BY completed_at DESC
		LIMIT 100`
	rowsArch, err := db.Query(queryArchive)
	if err != nil {
		return err
	}
	defer rowsArch.Close()

	var newArchive []ConsolidatedMessage
	for rowsArch.Next() {
		var m ConsolidatedMessage
		var doneInt int
		if err := rowsArch.Scan(&m.ID, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &doneInt, &m.CreatedAt, &m.CompletedAt); err != nil {
			return err
		}
		m.Done = doneInt == 1
		newArchive = append(newArchive, m)
		newKnownTS[m.SourceTS] = true
	}
	archiveCache = newArchive
	knownTS = newKnownTS

	return nil
}

func SaveMessage(msg ConsolidatedMessage) (bool, error) {
	cacheMu.RLock()
	if knownTS[msg.SourceTS] {
		cacheMu.RUnlock()
		return false, nil // Skip DB hit for known message
	}
	cacheMu.RUnlock()

	query := `INSERT INTO messages (source, room, task, requester, assignee, assigned_at, link, source_ts) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			  ON CONFLICT(source_ts) DO NOTHING;`
	res, err := db.Exec(query, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS)
	if err != nil {
		log.Printf("SaveMessage Error: %v", err)
		return false, err
	}

	rows, _ := res.RowsAffected()
	saved := rows > 0

	// Proactively update cache status instead of full refresh
	if saved {
		cacheMu.Lock()
		knownTS[msg.SourceTS] = true
		cacheMu.Unlock()
	}

	return saved, nil
}

func GetMessages() ([]ConsolidatedMessage, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if messageCache == nil {
		// If cache is empty, it might be first load or refresh failed
		return nil, fmt.Errorf("cache is empty")
	}
	return messageCache, nil
}

func MarkMessageDone(id int, done bool) error {
	val := 0
	var completeTime interface{} = nil
	if done {
		val = 1
		completeTime = time.Now()
	}
	_, err := db.Exec("UPDATE messages SET done = $1, completed_at = $2 WHERE id = $3", val, completeTime, id)
	if err == nil {
		// Force refresh cache on modification to stay consistent
		go RefreshCache()
	}
	return err
}

func GetArchivedMessages() ([]ConsolidatedMessage, error) {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return archiveCache, nil
}

func UpdateTaskText(id int, task string) error {
	_, err := db.Exec("UPDATE messages SET task = $1 WHERE id = $2", task, id)
	if err == nil {
		go RefreshCache()
	}
	return err
}
