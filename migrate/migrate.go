package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	sqliteDB, err := sql.Open("sqlite3", "messages.db")
	if err != nil {
		log.Fatalf("Failed to open SQLite: %v", err)
	}
	defer sqliteDB.Close()

	neonURL := os.Getenv("DATABASE_URL")
	if neonURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	neonDB, err := sql.Open("postgres", neonURL)
	if err != nil {
		log.Fatalf("Failed to open Neon: %v", err)
	}
	defer neonDB.Close()

	// 1. Create table in Neon if not exists
	_, err = neonDB.Exec(`
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
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		log.Fatalf("Failed to create Neon table: %v", err)
	}

	// 2. Read from SQLite
	rows, err := sqliteDB.Query("SELECT source, room, task, requester, assignee, assigned_at, link, source_ts, done, created_at FROM messages")
	if err != nil {
		log.Fatalf("Failed to query SQLite: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var source, room, task, requester, assignee, assignedAt, link, sourceTS string
		var done int
		var createdAt time.Time

		err := rows.Scan(&source, &room, &task, &requester, &assignee, &assignedAt, &link, &sourceTS, &done, &createdAt)
		if err != nil {
			log.Printf("Error scanning SQLite row: %v", err)
			continue
		}

		// 3. Insert into Neon
		_, err = neonDB.Exec(`
			INSERT INTO messages (source, room, task, requester, assignee, assigned_at, link, source_ts, done, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT(source_ts) DO NOTHING
		`, source, room, task, requester, assignee, assignedAt, link, sourceTS, done, createdAt)

		if err != nil {
			log.Printf("Error inserting into Neon: %v", err)
			continue
		}
		count++
	}

	fmt.Printf("Migration completed: %d messages copied to Neon.\n", count)
}
