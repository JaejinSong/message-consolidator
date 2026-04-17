package main

import (
	"context"
	"fmt"
	"log"
	"message-consolidator/config"
	"message-consolidator/store"
)

func runDBInspect(cfg *config.Config) {
	if err := store.InitDB(context.Background(), cfg); err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	db := store.GetDB()
	//Why: Inspects specific message IDs to debug missing source metadata or empty task descriptions that were reported in recent logs.
	rows, err := db.Query("SELECT id, source, source_ts, task, original_text FROM messages WHERE id IN (7772, 7762, 7763, 7009, 7010)")
	if err != nil {
		log.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	fmt.Println("ID | Source | SourceTS | Task | Has Original Text")
	fmt.Println("---|--------|----------|------|------------------")
	for rows.Next() {
		var id int
		var source, sourceTS, task string
		var originalText *string
		if err := rows.Scan(&id, &source, &sourceTS, &task, &originalText); err != nil {
			log.Printf("Scan failed: %v", err)
			continue
		}
		hasOriginal := "No"
		if originalText != nil && *originalText != "" {
			hasOriginal = "Yes"
		}
		fmt.Printf("%d | %s | %s | %s | %s\n", id, source, sourceTS, task, hasOriginal)
	}
}
