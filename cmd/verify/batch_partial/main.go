package main

import (
	"fmt"
	"log"
	"message-consolidator/ai"
	"message-consolidator/store"
	"message-consolidator/internal/testutil"
)

func main() {
	// 1. Setup Test DB
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		log.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	lang := "ko"
	
	// Seed messages
	for i := 1; i <= 3; i++ {
		_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, done, is_deleted, source_ts) VALUES (?, ?, ?, ?, ?, ?, ?)",
			i, "user@example.com", fmt.Sprintf("Task %d", i), "slack", 0, 0, fmt.Sprintf("ts_%d", i))
	}

	fmt.Println("--- [1] Mocking Partial Success AI Response ---")
	// Simulation of AI response: 1: Success, 2: ERROR, 3: Success
	newTrans := []ai.TranslationResult{
		{MessageID: 1, Text: "번역 1", Error: ""},
		{MessageID: 2, Text: "", Error: "번역 불가능한 특수 기호 포함"},
		{MessageID: 3, Text: "번역 3", Error: ""},
	}

	fmt.Println("\n--- [2] Filtering and Bulk Saving ---")
	successMap := make(map[int]string)
	for _, rt := range newTrans {
		if rt.Error == "" {
			successMap[rt.MessageID] = rt.Text
		}
	}

	if len(successMap) > 0 {
		err = store.SaveTaskTranslationsBulk(lang, successMap)
		if err != nil { fmt.Printf("Error during bulk save: %v\n", err) }
	}
	fmt.Printf("Bulk saved %d items.\n", len(successMap))

	fmt.Println("\n--- [3] Verification (DB Check) ---")
	cached, _ := store.GetTaskTranslationsBatch([]int{1, 2, 3}, lang)
	fmt.Printf("Cached items in DB: %v\n", cached)

	if _, ok := cached[2]; ok {
		log.Fatal("ERROR: ID 2 should NOT be in the cache because it had a translation error.")
	}
	if len(cached) != 2 {
		log.Fatalf("ERROR: Expected 2 items in DB, got %d", len(cached))
	}

	fmt.Println("\n--- [4] Verification (Response Simulation) ---")
	// simulate the response building logic
	for _, id := range []int{1, 2, 3} {
		text := cached[id]
		errorMsg := ""
		for _, nt := range newTrans {
			if nt.MessageID == id { errorMsg = nt.Error }
		}
		
		fmt.Printf("ID %d: Success=%v, Text='%s', Error='%s'\n", 
			id, text != "", text, errorMsg)
	}

	fmt.Println("\n[Success] Partial Success Model Verified.")
}
