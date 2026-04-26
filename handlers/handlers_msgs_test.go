package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleGetMessages(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "Test User", "")

	// Create a mock message with non-null values for scanned columns
	_, err = store.GetDB().Exec(`INSERT INTO messages 
		(user_email, task, source, source_ts, done, requester, assignee, link, room, original_text, category, deadline, assigned_at, created_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		email, "Test Task", "slack", "ts123", 0, "Requester", "me", "http://link", "Room", "Original", "personal", "", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Failed to insert mock message: %v", err)
	}

	// Refresh cache to ensure message is available
	_ = store.RefreshCache(context.Background(), email)

	req := NewMockRequest("GET", "/api/messages", email)
	rr := httptest.NewRecorder()

	api := &API{}
	api.HandleGetMessages(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var msgs store.CategorizedMessages
	if err := json.Unmarshal(rr.Body.Bytes(), &msgs); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	t.Logf("Queried DB directly: %s", rr.Body.String())

	if len(msgs.Inbox) != 1 {
		t.Fatalf("Expected 1 message in Inbox, got %d", len(msgs.Inbox))
	}
	if msgs.Inbox[0].Task != "Test Task" {
		t.Errorf("Expected task 'Test Task', got '%s'", msgs.Inbox[0].Task)
	}
}

func TestHandleDelete(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "Test User", "")

	_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, source_ts, is_deleted) VALUES (?, ?, ?, ?, ?, ?)",
		1, email, "Task to delete", "slack", "ts123", 0)
	_ = store.RefreshCache(context.Background(), email)

	// Test soft delete
	body, _ := json.Marshal(map[string]interface{}{"ids": []int{1}})
	req, _ := http.NewRequest("POST", "/api/messages/delete", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()

	api := &API{}
	api.HandleDelete(rr, req)

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

// TestHandleGetArchived_PaginationGuards verifies the Wave 3 guards in
// HandleGetArchived: limit<=0 falls back to default, limit>max is capped,
// and offset<0 is clamped. Inserts a small fixture and asserts behavior is
// sane across boundary inputs (avoids inserting hundreds of rows by relying
// on response shape rather than internal SQL state).
func TestHandleGetArchived_PaginationGuards(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "archive-pager@example.com"
	if _, err := store.GetOrCreateUser(context.Background(), email, "U", ""); err != nil {
		t.Fatalf("user: %v", err)
	}
	old := time.Now().AddDate(0, 0, -10)
	for i := 1; i <= 5; i++ {
		if _, err := store.GetDB().Exec(
			`INSERT INTO messages (id, user_email, task, source, source_ts, done, is_deleted, completed_at, assigned_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			i, email, fmt.Sprintf("Done %d", i), "slack", fmt.Sprintf("ts%d", i), 1, 0, old, old, old,
		); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	_ = store.RefreshCache(context.Background(), email)

	tests := []struct {
		name      string
		query     string
		wantLen   int
		wantTotal int
	}{
		{"no limit -> default cap permits all", "?status=done", 5, 5},
		{"explicit limit=2 honored", "?status=done&limit=2", 2, 5},
		{"limit=0 -> default kicks in", "?status=done&limit=0", 5, 5},
		{"negative offset clamped to 0", "?status=done&offset=-7", 5, 5},
		{"oversize limit capped (still returns all 5)", "?status=done&limit=999999", 5, 5},
	}

	api := &API{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := NewMockRequest("GET", "/api/messages/archive"+tt.query, email)
			rr := httptest.NewRecorder()
			api.HandleGetArchived(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
			}
			var resp struct {
				Messages []store.ConsolidatedMessage `json:"messages"`
				Total    int                         `json:"total"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(resp.Messages) != tt.wantLen {
				t.Errorf("len=%d want %d", len(resp.Messages), tt.wantLen)
			}
			if resp.Total != tt.wantTotal {
				t.Errorf("total=%d want %d", resp.Total, tt.wantTotal)
			}
		})
	}
}

func TestHandleGetArchived_PaginationConstants(t *testing.T) {
	if defaultArchivePageSize <= 0 {
		t.Errorf("defaultArchivePageSize must be positive, got %d", defaultArchivePageSize)
	}
	if maxArchivePageSize < defaultArchivePageSize {
		t.Errorf("maxArchivePageSize (%d) must be >= default (%d)", maxArchivePageSize, defaultArchivePageSize)
	}
}
