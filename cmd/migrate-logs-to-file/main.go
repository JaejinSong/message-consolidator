// One-shot migration: exports ai_inference_logs payload columns (original_text, raw_response)
// to JSONL file before the v9 schema migration drops them. Run on existing prod DB BEFORE
// deploying the v9 image. Idempotent: repeated runs append to the same output file.
//
// Usage:
//
//	go run ./cmd/migrate-logs-to-file [--dry-run] [--output /app/logs/ai_inference_export.jsonl]
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/tursodatabase/libsql-client-go/libsql"
)

type exportRow struct {
	ID           int64          `json:"id"`
	MessageID    sql.NullInt64  `json:"-"`
	MessageIDVal *int64         `json:"message_id,omitempty"`
	Source       sql.NullString `json:"-"`
	SourceVal    *string        `json:"source,omitempty"`
	OriginalText sql.NullString `json:"-"`
	OriginalVal  *string        `json:"original_text,omitempty"`
	RawResponse  sql.NullString `json:"-"`
	ResponseVal  *string        `json:"raw_response,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

func main() {
	dryRun := flag.Bool("dry-run", false, "print row count only, do not write file")
	output := flag.String("output", "/app/logs/ai_inference_export.jsonl", "output JSONL path")
	flag.Parse()

	dbURL := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")
	if dbURL == "" {
		log.Fatal("TURSO_DATABASE_URL not set")
	}

	connector, err := libsql.NewConnector(dbURL, libsql.WithAuthToken(token))
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	conn := sql.OpenDB(connector)
	defer conn.Close()

	rows, err := conn.Query(
		`SELECT id, message_id, source, original_text, raw_response, created_at
		 FROM ai_inference_logs
		 WHERE original_text IS NOT NULL OR raw_response IS NOT NULL
		 ORDER BY id`,
	)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	defer rows.Close()

	if *dryRun {
		var n int
		for rows.Next() {
			n++
		}
		fmt.Printf("dry-run: %d rows with payload found\n", n)
		return
	}

	f, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("open output: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)

	var exported int
	for rows.Next() {
		var r exportRow
		var createdStr string
		if err := rows.Scan(&r.ID, &r.MessageID, &r.Source, &r.OriginalText, &r.RawResponse, &createdStr); err != nil {
			log.Printf("scan row: %v — skipping", err)
			continue
		}
		r.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if r.MessageID.Valid {
			r.MessageIDVal = &r.MessageID.Int64
		}
		if r.Source.Valid {
			r.SourceVal = &r.Source.String
		}
		if r.OriginalText.Valid {
			r.OriginalVal = &r.OriginalText.String
		}
		if r.RawResponse.Valid {
			r.ResponseVal = &r.RawResponse.String
		}
		if err := enc.Encode(r); err != nil {
			log.Printf("encode row %d: %v — skipping", r.ID, err)
			continue
		}
		exported++
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("rows iteration: %v", err)
	}
	fmt.Printf("exported %d rows → %s\n", exported, *output)
}
