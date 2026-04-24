package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"message-consolidator/config"
	"strings"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func runDedupTasks(cfg *config.Config) {
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

	ctx := context.Background()
	rows, err := queryDupRows(ctx, db)
	if err != nil {
		fmt.Printf("query error: %v\n", err)
		return
	}
	fmt.Printf("Found %d rows to process\n", len(rows))

	fixed := 0
	for _, r := range rows {
		cleaned := dedupTaskSections(r.task)
		if cleaned == r.task {
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE messages SET task = ? WHERE id = ?`, cleaned, r.id); err != nil {
			fmt.Printf("  [FAIL] id=%d: %v\n", r.id, err)
			continue
		}
		fmt.Printf("  [OK] id=%d cleaned\n", r.id)
		fixed++
	}
	fmt.Printf("Done: %d/%d rows updated\n", fixed, len(rows))
}

type dupRow struct {
	id   int64
	task string
}

func queryDupRows(ctx context.Context, db *sql.DB) ([]dupRow, error) {
	sqlRows, err := db.QueryContext(ctx, `SELECT id, task FROM messages WHERE task LIKE '%[Update:%[Update:%'`)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var result []dupRow
	for sqlRows.Next() {
		var r dupRow
		if err := sqlRows.Scan(&r.id, &r.task); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, sqlRows.Err()
}

// dedupTaskSections removes duplicate content sections separated by "--- [Update: ...] ---" markers.
func dedupTaskSections(task string) string {
	const sep = "\n\n--- ["
	parts := strings.Split(task, sep)
	seen := make(map[string]bool)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		key := strings.TrimSpace(sectionContent(p))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return strings.Join(out, sep)
}

// sectionContent strips the "Update: date] ---\n" header to get comparable content.
func sectionContent(section string) string {
	if idx := strings.Index(section, "] ---\n"); idx != -1 {
		return section[idx+6:]
	}
	return section
}
