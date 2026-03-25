package handlers

import (
	"bytes"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleGetMessages(t *testing.T) {
	cleanup, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	_, _ = store.GetOrCreateUser(email, "Test User", "")

	// Create a mock message with non-null values for scanned columns
	_, err = store.GetDB().Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, requester, assignee, link, room, original_text, category, deadline, assigned_at, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Test Task", "slack", "ts123", 0, "Requester", "Assignee", "http://link", "Room", "Original", "todo", "", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Failed to insert mock message: %v", err)
	}

	// Refresh cache to ensure message is available
	_ = store.RefreshCache(email)

	req := NewMockRequest("GET", "/api/messages", email)
	rr := httptest.NewRecorder()

	HandleGetMessages(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var msgs []store.ConsolidatedMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	t.Logf("Queried DB directly: %s", rr.Body.String())

	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Task != "Test Task" {
		t.Errorf("Expected task 'Test Task', got '%s'", msgs[0].Task)
	}
}

func TestHandleDelete(t *testing.T) {
	cleanup, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	_, _ = store.GetOrCreateUser(email, "Test User", "")

	_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, source_ts, is_deleted) VALUES (?, ?, ?, ?, ?, ?)",
		1, email, "Task to delete", "slack", "ts123", 0)
	_ = store.RefreshCache(email)

	// Test soft delete
	body, _ := json.Marshal(map[string]interface{}{"ids": []int{1}})
	req, _ := http.NewRequest("POST", "/api/messages/delete", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()

	HandleDelete(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Verify soft delete in DB
	var isDeleted bool
	_ = store.GetDB().QueryRow("SELECT is_deleted FROM messages WHERE id = 1").Scan(&isDeleted)
	if !isDeleted {
		t.Error("Expected message to be soft-deleted")
	}
}
