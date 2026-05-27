package store

import (
	"message-consolidator/internal/testutil"
	"testing"
)

func TestLogAIInference(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(InitDB, ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	err = LogAIInference(1, "test_source")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	var mID int
	var source string
	err = GetDB().QueryRow("SELECT message_id, source FROM ai_inference_logs").Scan(&mID, &source)
	if err != nil {
		t.Fatalf("Failed to query inserted data: %v", err)
	}

	if mID != 1 || source != "test_source" {
		t.Errorf("Data mismatch: message_id=%d source=%s", mID, source)
	}
}
