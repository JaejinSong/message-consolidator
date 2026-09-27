package services

import (
	"context"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"testing"
	"time"

	"message-consolidator/types"
)

func setupWADBLoggerTest(t *testing.T) func() {
	t.Helper()
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	return cleanup
}

// TestWADBLoggerReceive_PersistsRawJSON pins that Receive stores the marshaled RawMessage
// so a durable replay path can rebuild the message without re-fetching from the channel.
func TestWADBLoggerReceive_PersistsRawJSON(t *testing.T) {
	cleanup := setupWADBLoggerTest(t)
	defer cleanup()

	logger := NewWADBLogger()
	email := testutil.RandomEmail("wa-db-logger")
	msg := types.RawMessage{
		ID:        testutil.RandomID("msg"),
		Sender:    "sender-1",
		Text:      "hello world",
		Timestamp: time.Now(),
	}

	logger.Receive(email, "chat-jid-1", msg)

	var rawJSON string
	if err := store.GetDB().QueryRowContext(context.Background(),
		`SELECT raw_json FROM wa_messages WHERE message_id = ?`, msg.ID,
	).Scan(&rawJSON); err != nil {
		t.Fatalf("query raw_json: %v", err)
	}
	if rawJSON == "" {
		t.Fatal("expected raw_json to be persisted, got empty string")
	}

	var decoded types.RawMessage
	if err := json.Unmarshal([]byte(rawJSON), &decoded); err != nil {
		t.Fatalf("raw_json did not round-trip as a RawMessage: %v", err)
	}
	if decoded.ID != msg.ID || decoded.Text != msg.Text {
		t.Errorf("decoded raw_json mismatch: got %+v, want ID=%q Text=%q", decoded, msg.ID, msg.Text)
	}
}
