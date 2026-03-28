package handlers

import (
	"encoding/json"
	"message-consolidator/auth"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"context"
	"message-consolidator/config"
)

func TestArchiveFilteringAndPriority(t *testing.T) {
	cleanup, err := testutil.SetupTestDB()
	if err != nil {
		t.Fatalf("Failed to setup test DB: %v", err)
	}
	defer cleanup()

	email := "test@example.com"
	_, _ = store.GetOrCreateUser(email, "Test User", "")

	now := time.Now()
	oldDate := now.AddDate(0, 0, -10)
	// 1. Only Done
	if _, err := store.GetDB().Exec(`INSERT INTO messages (id, user_email, task, source, done, is_deleted, completed_at, assigned_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, email, "Done Task", "slack", 1, 0, oldDate, oldDate, oldDate); err != nil {
		t.Fatalf("Insert 1 failed: %v", err)
	}
	
	// 2. Only Deleted
	if _, err := store.GetDB().Exec(`INSERT INTO messages (id, user_email, task, source, done, is_deleted, assigned_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		2, email, "Deleted Task", "slack", 0, 1, oldDate, oldDate); err != nil {
		t.Fatalf("Insert 2 failed: %v", err)
	}

	// 3. Both Done and Deleted (Priority Test)
	if _, err := store.GetDB().Exec(`INSERT INTO messages (id, user_email, task, source, done, is_deleted, completed_at, assigned_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		3, email, "Done but Deleted Task", "slack", 1, 1, oldDate, oldDate, oldDate); err != nil {
		t.Fatalf("Insert 3 failed: %v", err)
	}

	// Double check what's in the DB
	var count int
	store.GetDB().QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	t.Logf("Total messages in DB: %d", count)

	_ = store.RefreshCache(email)
	cfg := &config.Config{AuthDisabled: true}
	api := &API{Config: cfg}

	// Test Status = All (Should return all 3)
	t.Run("StatusAll", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/messages/archive?status=all", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleGetArchived(rr, req)
		
		if rr.Code != http.StatusOK {
			t.Fatalf("StatusAll failed with status %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Messages []store.ConsolidatedMessage `json:"messages"`
			Total    int                         `json:"total"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Failed to unmarshal response: %v", err)
		}
		if resp.Total != 3 {
			t.Errorf("Expected 3 messages in 'all', got %d. Body: %s", resp.Total, rr.Body.String())
		}
		if len(resp.Messages) != 3 {
			t.Errorf("Expected 3 messages data in 'all', got %d", len(resp.Messages))
		}
	})

	// Test Status = Done (Should return ID 1 and 3 - All completed items)
	t.Run("StatusDone", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/messages/archive?status=done", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleGetArchived(rr, req)
		
		var resp struct {
			Messages []store.ConsolidatedMessage `json:"messages"`
			Total    int                         `json:"total"`
		}
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Total != 2 {
			t.Errorf("Expected 2 messages in 'done', got %d. Body: %s", resp.Total, rr.Body.String())
		}
	})
	
	// Test Status = Canceled (Should return ID 2 - Uncompleted but deleted)
	t.Run("StatusCanceled", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/messages/archive?status=canceled", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleGetArchived(rr, req)
		
		var resp struct {
			Messages []store.ConsolidatedMessage `json:"messages"`
			Total    int                         `json:"total"`
		}
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Total != 1 {
			t.Errorf("Expected 1 message in 'canceled', got %d. Body: %s", resp.Total, rr.Body.String())
		} else if resp.Messages[0].ID != 2 {
			t.Errorf("Expected ID 2 in 'canceled', got ID %d", resp.Messages[0].ID)
		}
	})

	// Test Export Excel with Status = Done
	t.Run("ExportExcelStatusDone", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/messages/export/excel?status=done", nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.UserEmailKey, email))
		rr := httptest.NewRecorder()
		api.HandleExportExcel(rr, req)
		
		if rr.Code != http.StatusOK {
			t.Errorf("Export failed with status %d", rr.Code)
		}
		// Content validation of excel is complex, but ensuring it returns 200 with the filter applied internally is a good start.
	})
}
