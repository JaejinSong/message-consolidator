package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func main() {
	// Turso DB URL and Token
	dbURL := "libsql://aijj-jinronara.aws-ap-northeast-1.turso.io"
	authToken := "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.eyJhIjoicnciLCJpYXQiOjE3NzQyNjY2NTgsImlkIjoiMDE5ZDFhODgtNjEwMS03ZjI1LWI4MzYtZjgyZjc5ODg3ZWIzIiwicmlkIjoiYjk4N2JmMTEtMjY3Ny00OWZmLTljY2QtMDMwNzkxZDVhNzE3In0._yrZbjKR9EAdlL4fJf-sUnII3U2c_DYDiMyjbRQSuXfAuIEmbqq80U-94I0Ai2nYP3J2tlQy1jE8gJAIDbA3DA"

	url := fmt.Sprintf("%s?authToken=%s", dbURL, authToken)
	db, err := sql.Open("libsql", url)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	email := "jjsong@whatap.io"
	// 2026-03-20 00:00:00 UTC
	newTS := "1773878400"

	// Correct column names: user_email, source, target_id, last_ts
	result, err := db.Exec("UPDATE scan_metadata SET last_ts = ? WHERE user_email = ? AND source = 'gmail' AND target_id = 'inbox'", newTS, email)
	if err != nil {
		log.Fatalf("Failed to update scan metadata: %v", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		fmt.Printf("No existing row found. Inserting new one...\n")
		_, err := db.Exec("INSERT INTO scan_metadata (user_email, source, target_id, last_ts) VALUES (?, 'gmail', 'inbox', ?)", email, newTS)
		if err != nil {
			log.Fatalf("Failed to insert scan metadata: %v", err)
		}
		fmt.Printf("Successfully inserted checkpoint for %s to %s\n", email, newTS)
	} else {
		fmt.Printf("Successfully reset checkpoint for %s to %s\n", email, newTS)
	}
}
