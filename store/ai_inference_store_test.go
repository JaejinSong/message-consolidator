package store

import (
	"message-consolidator/config"
	"os"
	"testing"
)

func TestLogAIInference(t *testing.T) {
	// Setup: Temporary Test DB
	dbPath := "test_ai_inference.db"
	defer os.Remove(dbPath)

	cfg := &config.Config{TursoURL: "file:" + dbPath}
	if err := InitDB(cfg); err != nil {
		t.Fatalf("Failed to init DB: %v", err)
	}

	// Test case: Successful Logging
	err := LogAIInference(1, "test_source", "test input", "{\"result\": \"success\"}")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Verify
	var original, raw string
	var mID int
	err = db.QueryRow("SELECT message_id, original_text, raw_response FROM ai_inference_logs").Scan(&mID, &original, &raw)
	if err != nil {
		t.Fatalf("Failed to query inserted data: %v", err)
	}

	if mID != 1 || original != "test input" || raw != "{\"result\": \"success\"}" {
		t.Errorf("Data mismatch: %d, %s, %s", mID, original, raw)
	}
}
