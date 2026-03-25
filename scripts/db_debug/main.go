package main

import (
	"database/sql"
	"fmt"
	"log"
	"message-consolidator/config"
	"os"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func main() {
	cfg := config.LoadConfig()
	if cfg.TursoURL == "" {
		log.Fatal("TURSO_DATABASE_URL is not set")
	}

	dbURL := cfg.TursoURL
	if cfg.TursoToken != "" {
		dbURL = fmt.Sprintf("%s?authToken=%s", dbURL, cfg.TursoToken)
	}

	db, err := sql.Open("libsql", dbURL)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	email := os.Getenv("DEFAULT_USER_EMAIL")
	if email == "" {
		email = "jjsong@whatap.io"
	}

	fmt.Printf("--- Diagnosing for user: %s ---\n", email)

	// 1. Total counts
	var total, done, deleted int
	_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = ?", email).Scan(&total)
	_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = ? AND done = 1 AND is_deleted = 0", email).Scan(&done)
	_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = ? AND is_deleted = 1", email).Scan(&deleted)

	var allDone int
	_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE user_email = ? AND done = 1", email).Scan(&allDone)

	fmt.Printf("Total: %d | Done (Active): %d | Done (Total): %d | Deleted: %d\n", total, done, allDone, deleted)

	// 2. Sample completed_at format
	if done > 0 {
		var sample string
		_ = db.QueryRow("SELECT completed_at FROM messages WHERE user_email = ? AND done = 1 AND completed_at IS NOT NULL LIMIT 1", email).Scan(&sample)
		fmt.Printf("Sample completed_at: '%s'\n", sample)

		var hr string
		_ = db.QueryRow("SELECT strftime('%H', completed_at) FROM messages WHERE user_email = ? AND done = 1 AND completed_at IS NOT NULL LIMIT 1", email).Scan(&hr)
		fmt.Printf("strftime('%%H', sample): '%s'\n", hr)
	}

	// 3. Check for any messages at all (to see if email matches)
	if total == 0 {
		fmt.Println("No messages found for this email. Listing unique emails in DB:")
		rows, _ := db.Query("SELECT DISTINCT user_email FROM messages LIMIT 10")
		if rows != nil {
			defer rows.Close()
			for rows.Next() {
				var e string
				_ = rows.Scan(&e)
				fmt.Printf(" - %s\n", e)
			}
		}
	}
}
