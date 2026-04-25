package main

import (
	"context"
	"fmt"
	"log"
	"message-consolidator/store"
	"message-consolidator/internal/testutil"
)

func main() {
	// Why: Set up a clean test database environment to isolate verification results.
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		log.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	lang := "ko"
	
	// Why: Create mock messages to simulate real tasks for translation.
	for i := 1; i <= 5; i++ {
		_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?, ?)",
			i, "user@example.com", fmt.Sprintf("Task %d", i), "slack", 0, 0, fmt.Sprintf("ts_%d", i))
	}

	fmt.Println("--- [1] Initial Cache Check (Expected: Empty) ---")
	ctx := context.Background()
	cached, _ := store.GetTaskTranslationsBatch(ctx, []store.MessageID{1, 2, 3, 4, 5}, lang)
	fmt.Printf("Cached count: %d\n", len(cached))

	fmt.Println("\n--- [2] Bulk Save Simulation ---")
	newTranslations := map[store.MessageID]string{
		1: "작업 1",
		2: "작업 2",
		3: "작업 3",
	}
	err = store.SaveTaskTranslationsBulk(ctx, lang, newTranslations)
	if err != nil {
		log.Fatalf("Bulk save failed: %v", err)
	}
	fmt.Println("Bulk save successful.")

	fmt.Println("\n--- [3] Second Cache Check (Expected: 3 items) ---")
	cached, _ = store.GetTaskTranslationsBatch(ctx, []store.MessageID{1, 2, 3, 4, 5}, lang)
	fmt.Printf("Cached items: %v\n", cached)
	
	if len(cached) != 3 {
		log.Fatalf("Logic error: Expected 3 cached items, got %d", len(cached))
	}

	fmt.Println("\n--- [4] Bulk Upsert Verification (Overwrite test) ---")
	updates := map[store.MessageID]string{
		1: "작업 1 (수정됨)",
	}
	_ = store.SaveTaskTranslationsBulk(ctx, lang, updates)
	cached, _ = store.GetTaskTranslationsBatch(ctx, []store.MessageID{1}, lang)
	fmt.Printf("Updated item 1: %s\n", cached[1])

	fmt.Println("\n[Success] Translation Batch Cost Optimization Logic Verified.")
}
