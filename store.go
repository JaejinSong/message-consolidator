package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

type ConsolidatedMessage struct {
	ID         int       `json:"id"`
	Source     string    `json:"source"` // slack or whatsapp
	Room       string    `json:"room"`
	Task       string    `json:"task"`
	Requester  string    `json:"requester"`
	Assignee   string    `json:"assignee"`
	AssignedAt string    `json:"assigned_at"`
	Link       string    `json:"link"`
	SourceTS   string    `json:"source_ts"`
	Done       bool      `json:"done"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

var db *sql.DB

func InitDB(connStr string) error {
	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

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

	log.Println("Database initialized with Neon")
	return nil
}

func SaveMessage(msg ConsolidatedMessage) error {
	query := `INSERT INTO messages (source, room, task, requester, assignee, assigned_at, link, source_ts) 
			  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			  ON CONFLICT(source_ts) DO NOTHING;`
	_, err := db.Exec(query, msg.Source, msg.Room, msg.Task, msg.Requester, msg.Assignee, msg.AssignedAt, msg.Link, msg.SourceTS)
	if err != nil {
		log.Printf("SaveMessage Error: %v", err)
	}
	return err
}

func GetMessages() ([]ConsolidatedMessage, error) {
	// Only fetch non-done tasks OR tasks completed within 7 days
	query := `
		SELECT id, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, done, created_at, completed_at 
		FROM messages 
		WHERE done = 0 OR (done = 1 AND (completed_at IS NULL OR completed_at > NOW() - INTERVAL '7 days'))
		ORDER BY created_at DESC 
		LIMIT 200`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		var m ConsolidatedMessage
		var doneInt int
		err := rows.Scan(&m.ID, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &doneInt, &m.CreatedAt, &m.CompletedAt)
		m.Done = doneInt == 1
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
	
func MarkMessageDone(id int, done bool) error {
	val := 0
	var completeTime interface{} = nil
	if done {
		val = 1
		completeTime = time.Now()
	}
	_, err := db.Exec("UPDATE messages SET done = $1, completed_at = $2 WHERE id = $3", val, completeTime, id)
	return err
}

func GetArchivedMessages() ([]ConsolidatedMessage, error) {
	// Fetch tasks completed more than 7 days ago
	query := `
		SELECT id, source, COALESCE(room, ''), task, requester, assignee, assigned_at, link, source_ts, done, created_at, completed_at 
		FROM messages 
		WHERE done = 1 AND completed_at IS NOT NULL AND completed_at <= NOW() - INTERVAL '7 days'
		ORDER BY completed_at DESC`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ConsolidatedMessage
	for rows.Next() {
		var m ConsolidatedMessage
		var doneInt int
		err := rows.Scan(&m.ID, &m.Source, &m.Room, &m.Task, &m.Requester, &m.Assignee, &m.AssignedAt, &m.Link, &m.SourceTS, &doneInt, &m.CreatedAt, &m.CompletedAt)
		m.Done = doneInt == 1
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func UpdateTaskText(id int, task string) error {
	_, err := db.Exec("UPDATE messages SET task = $1 WHERE id = $2", task, id)
	return err
}
