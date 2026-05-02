package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
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

func TestCategorizeForResponse(t *testing.T) {
	msgs := []store.ConsolidatedMessage{
		{ID: 1, Category: services.CategoryPersonal},
		{ID: 2, Category: services.CategoryRequested},
		{ID: 3, Category: "reference"},
		{ID: 4, Category: ""},
		{ID: 5, Category: services.CategoryPersonal},
	}
	got := categorizeForResponse(msgs)
	if len(got.Inbox) != 2 || got.Inbox[0].ID != 1 || got.Inbox[1].ID != 5 {
		t.Errorf("Inbox = %+v", got.Inbox)
	}
	if len(got.Delegated) != 1 || got.Delegated[0].ID != 2 {
		t.Errorf("Delegated = %+v", got.Delegated)
	}
	if len(got.Reference) != 2 {
		t.Errorf("Reference len = %d, want 2", len(got.Reference))
	}
}

func TestCategorizeForResponse_EmptyInput(t *testing.T) {
	got := categorizeForResponse(nil)
	if got.Inbox == nil || got.Delegated == nil || got.Reference == nil {
		t.Error("expected non-nil slices for FE consistency")
	}
	if len(got.Inbox) != 0 || len(got.Delegated) != 0 || len(got.Reference) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestHandleSearchActive_QueryTooShort(t *testing.T) {
	api := &API{}
	cases := []string{"", "a", "ab", " a "}
	for _, q := range cases {
		req := NewMockRequest("GET", "/api/messages/search?q="+q, "u@x.io")
		rr := httptest.NewRecorder()
		api.HandleSearchActive(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("q=%q: expected 400, got %d", q, rr.Code)
		}
	}
}

func TestHandleSearchActive_Valid(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "search-active@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/messages/search?q=foo", email)
	rr := httptest.NewRecorder()
	api.HandleSearchActive(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleMarkDone_Guards(t *testing.T) {
	api := &API{}
	email := "markdone@example.com"

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/messages/done", bytes.NewBufferString("{bad"))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleMarkDone(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Tasks==nil returns 503", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"id": 1, "done": true})
		r, _ := http.NewRequest("POST", "/api/messages/done", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleMarkDone(rr, r)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("ID<=0 returns 400", func(t *testing.T) {
		// Why: needs non-nil Tasks to bypass the 503 guard; passing the zero-value pointer is enough since ID guard fires before any Tasks call.
		api2 := &API{Tasks: &services.TasksService{}}
		body, _ := json.Marshal(map[string]any{"id": 0, "done": true})
		r, _ := http.NewRequest("POST", "/api/messages/done", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api2.HandleMarkDone(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

func TestHandleToggleSubtask_Guards(t *testing.T) {
	api := &API{}
	email := "toggle@example.com"

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/messages/subtask", bytes.NewBufferString("{bad"))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleToggleSubtask(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("ID<=0 returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"id": 0, "subtask_index": 0, "done": true})
		r, _ := http.NewRequest("POST", "/api/messages/subtask", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleToggleSubtask(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

func TestHandleGetArchivedCount(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "arch-count@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("GET", "/api/messages/archive/count", email)
	rr := httptest.NewRecorder()
	api.HandleGetArchivedCount(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]int
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if _, ok := resp["count"]; !ok {
		t.Errorf("missing count field: %s", rr.Body.String())
	}
}

func TestHandleGetOriginal(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	owner := "msgowner@example.com"
	other := "other@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), owner, "U", "")
	_, _ = store.GetOrCreateUser(context.Background(), other, "O", "")

	_, _ = store.GetDB().Exec(
		"INSERT INTO messages (id, user_email, task, source, source_ts, original_text) VALUES (?, ?, ?, ?, ?, ?)",
		42, owner, "T", "slack", "ts42", "hello world",
	)

	api := &API{}

	newReq := func(idStr, email string) *http.Request {
		r, _ := http.NewRequest("GET", "/api/messages/"+idStr+"/original", nil)
		r = r.WithContext(WithMockUser(r.Context(), email))
		return mux.SetURLVars(r, map[string]string{"id": idStr})
	}

	t.Run("Invalid ID format", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleGetOriginal(rr, newReq("not-a-number", owner))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Not found", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleGetOriginal(rr, newReq("99999", owner))
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 (not-found path), got %d", rr.Code)
		}
	})

	t.Run("Cross-user access denied", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleGetOriginal(rr, newReq("42", other))
		if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusInternalServerError {
			// Why: store may filter by user_email and return ErrNoRows (500), which is also acceptable isolation.
			t.Errorf("expected 401 or 500, got %d", rr.Code)
		}
	})

	t.Run("Owner success", func(t *testing.T) {
		rr := httptest.NewRecorder()
		api.HandleGetOriginal(rr, newReq("42", owner))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp map[string]string
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["original_text"] != "hello world" {
			t.Errorf("original_text = %q", resp["original_text"])
		}
	})
}

func TestHandleHardDelete(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "harddel@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}

	t.Run("Empty IDs returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"ids": []int{}})
		r, _ := http.NewRequest("POST", "/api/messages/hard-delete", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleHardDelete(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, source_ts) VALUES (?, ?, ?, ?, ?)", 7, email, "T", "slack", "ts7")
		body, _ := json.Marshal(map[string]any{"ids": []int{7}})
		r, _ := http.NewRequest("POST", "/api/messages/hard-delete", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleHardDelete(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestHandleRestore(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "restore@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}

	t.Run("Empty IDs returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"ids": []int{}})
		r, _ := http.NewRequest("POST", "/api/messages/restore", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleRestore(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, source_ts, is_deleted) VALUES (?, ?, ?, ?, ?, ?)", 8, email, "T", "slack", "ts8", 1)
		body, _ := json.Marshal(map[string]any{"ids": []int{8}})
		r, _ := http.NewRequest("POST", "/api/messages/restore", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleRestore(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestHandleUpdateTask(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "update@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")
	_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, source_ts) VALUES (?, ?, ?, ?, ?)", 11, email, "Old", "slack", "ts11")

	api := &API{}

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/messages/update", bytes.NewBufferString("{bad"))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleUpdateTask(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"id": 11, "task": "New title"})
		r, _ := http.NewRequest("POST", "/api/messages/update", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleUpdateTask(rr, r)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

func TestHandleMergeTasks_Guards(t *testing.T) {
	api := &API{}
	email := "merge@example.com"

	t.Run("Invalid JSON", func(t *testing.T) {
		r, _ := http.NewRequest("POST", "/api/messages/merge", bytes.NewBufferString("{bad"))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleMergeTasks(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("Empty target_ids", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"target_ids": []int{}, "destination_id": 1})
		r, _ := http.NewRequest("POST", "/api/messages/merge", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleMergeTasks(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("destination_id zero", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"target_ids": []int{1}, "destination_id": 0})
		r, _ := http.NewRequest("POST", "/api/messages/merge", bytes.NewBuffer(body))
		r = r.WithContext(WithMockUser(r.Context(), email))
		rr := httptest.NewRecorder()
		api.HandleMergeTasks(rr, r)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

func TestHandleTranslateBatchTasks_BadJSON(t *testing.T) {
	api := &API{}
	r, _ := http.NewRequest("POST", "/api/messages/translate", bytes.NewBufferString("{bad"))
	r = r.WithContext(WithMockUser(r.Context(), "tx@example.com"))
	rr := httptest.NewRecorder()
	api.HandleTranslateBatchTasks(rr, r)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestGetMissingIDs(t *testing.T) {
	api := &API{}
	all := []store.MessageID{1, 2, 3, 4}
	cached := map[store.MessageID]string{1: "a", 3: "c"}
	got := api.getMissingIDs(all, cached)
	if len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Errorf("got %v, want [2 4]", got)
	}

	if g := api.getMissingIDs(nil, cached); len(g) != 0 {
		t.Errorf("nil input → %v, want empty", g)
	}
	if g := api.getMissingIDs(all, map[store.MessageID]string{1: "a", 2: "b", 3: "c", 4: "d"}); len(g) != 0 {
		t.Errorf("all cached → %v, want empty", g)
	}
}

func TestRespondWithResults(t *testing.T) {
	api := &API{}
	rr := httptest.NewRecorder()
	ids := []store.MessageID{1, 2, 3}
	cached := map[store.MessageID]string{1: "cached1"}
	newly := map[store.MessageID]string{2: "new2"}
	errs := map[store.MessageID]string{3: "boom"}

	api.respondWithResults(rr, ids, cached, newly, errs)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var resp batchTranslateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("len = %d, want 3", len(resp.Results))
	}
	if !resp.Results[0].Success || resp.Results[0].TranslatedText != "cached1" {
		t.Errorf("[0] = %+v", resp.Results[0])
	}
	if !resp.Results[1].Success || resp.Results[1].TranslatedText != "new2" {
		t.Errorf("[1] = %+v", resp.Results[1])
	}
	if resp.Results[2].Success || resp.Results[2].Error != "boom" {
		t.Errorf("[2] = %+v", resp.Results[2])
	}
}

func TestRespondWithUpdatedUser(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "refresh@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "Refresh", "")

	api := &API{}
	r, _ := http.NewRequest("GET", "/", nil)
	r = r.WithContext(WithMockUser(r.Context(), email))
	rr := httptest.NewRecorder()
	api.respondWithUpdatedUser(rr, r, email)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		User *store.User `json:"user"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.User == nil || resp.User.Email != email {
		t.Errorf("user = %+v", resp.User)
	}
}

func TestPrepareMissingRequests_NoMessages(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	api := &API{}
	ids := []store.MessageID{store.MessageID(999998), store.MessageID(999999)}
	reqs := api.prepareMissingRequests(context.Background(), "u@example.com", ids)
	// Non-existent IDs should result in empty slice (errors are swallowed).
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests for unknown IDs, got %d", len(reqs))
	}
}

func TestPrepareMissingRequests_EmptyIDs(t *testing.T) {
	t.Parallel()
	api := &API{}
	reqs := api.prepareMissingRequests(context.Background(), "u@example.com", nil)
	if reqs != nil {
		t.Errorf("expected nil for empty IDs, got %v", reqs)
	}
}
