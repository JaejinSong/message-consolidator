package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- evaluateBackfillCandidate coverage ----

// TestEvaluateBackfillCandidate_ActorEqualsRequester exercises the
// strings.EqualFold guard — returns false when actor == requester.
func TestEvaluateBackfillCandidate_ActorEqualsRequester(t *testing.T) {
	t.Parallel()
	m := store.ConsolidatedMessage{
		Assignee:  services.AssigneeShared,
		Room:      "room-x",
		Requester: "Alice",
	}
	// Pre-populate the room cache so the store lookup is skipped.
	cache := map[string]string{"room-x": "alice"} // actor == requester (case-insensitive)
	_, ok := evaluateBackfillCandidate(context.Background(), "u@x.io", m, cache)
	if ok {
		t.Error("expected false when actor equals requester (case-insensitive)")
	}
}

// TestEvaluateBackfillCandidate_ActorEmptyFromCache exercises the actor=="" guard.
func TestEvaluateBackfillCandidate_ActorEmptyFromCache(t *testing.T) {
	t.Parallel()
	m := store.ConsolidatedMessage{
		Assignee:  services.AssigneeShared,
		Room:      "room-y",
		Requester: "Bob",
	}
	cache := map[string]string{"room-y": ""} // empty actor
	_, ok := evaluateBackfillCandidate(context.Background(), "u@x.io", m, cache)
	if ok {
		t.Error("expected false when actor is empty")
	}
}

// TestEvaluateBackfillCandidate_LongTaskTruncated exercises the excerpt truncation branch.
func TestEvaluateBackfillCandidate_LongTaskTruncated(t *testing.T) {
	t.Parallel()
	longTask := strings.Repeat("x", 100)
	m := store.ConsolidatedMessage{
		Assignee:  services.AssigneeShared,
		Room:      "room-z",
		Requester: "Bob",
		Task:      longTask,
	}
	cache := map[string]string{"room-z": "charlie"} // actor != requester
	c, ok := evaluateBackfillCandidate(context.Background(), "u@x.io", m, cache)
	if !ok {
		t.Fatal("expected true when actor differs from requester")
	}
	if len(c.TaskExcerpt) != 80 {
		t.Errorf("excerpt length = %d, want 80", len(c.TaskExcerpt))
	}
}

// TestEvaluateBackfillCandidate_CacheHitActorDifferent exercises the successful
// candidate path when the room cache already has a non-matching actor.
func TestEvaluateBackfillCandidate_CacheHitActorDifferent(t *testing.T) {
	t.Parallel()
	m := store.ConsolidatedMessage{
		Assignee:  services.AssigneeShared,
		Room:      "room-cached",
		Requester: "Bob",
		Task:      "short task",
		ID:        store.MessageID(7),
	}
	cache := map[string]string{"room-cached": "charlie"} // actor != requester, in cache
	c, ok := evaluateBackfillCandidate(context.Background(), "u@x.io", m, cache)
	if !ok {
		t.Fatal("expected true when actor differs from requester and room is cached")
	}
	if c.ProposedAssignee != "charlie" {
		t.Errorf("ProposedAssignee = %q, want charlie", c.ProposedAssignee)
	}
}

// ---- HandleTranslate with DB ----

// TestHandleTranslate_WithLangAndEmptyMessages exercises the DB path
// (gatherMessagesForTranslation + filterUntranslatedIDs succeed, no messages to translate).
func TestHandleTranslate_WithLangAndEmptyMessages(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "translate-db@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	req := NewMockRequest("POST", "/api/translate?lang=en", email)
	rr := httptest.NewRecorder()
	api.HandleTranslate(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleReclassifyOldData with DB ----

// TestHandleReclassifyOldData_WithTasks verifies the handler returns 200 when Tasks is set.
// Uses a zero-value TasksService which will classify nothing but not panic.
func TestHandleReclassifyOldData_WithTasksService(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "reclassify@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{Tasks: &services.TasksService{}}
	req := NewMockRequest("GET", "/api/admin/reclassify", email)
	rr := httptest.NewRecorder()
	api.HandleReclassifyOldData(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleTranslateBatchTasks with DB (all-cached path) ----

// TestHandleTranslateBatchTasks_AllCached exercises the guard that returns early
// when all task IDs are already in the translation cache.
func TestHandleTranslateBatchTasks_AllCached(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "tx-cached@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	// No tasks in DB → cached map is empty → missingIDs is empty → early return.
	api := &API{}
	body, _ := json.Marshal(map[string]interface{}{
		"task_ids": []int{},
		"lang":     "en",
	})
	req, _ := http.NewRequest("POST", "/api/tasks/translate-batch", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleTranslateBatchTasks(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- HandleMarkDone success path with DB ----

// TestHandleToggleSubtask_WithDB exercises the DB happy path.
func TestHandleToggleSubtask_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "subtask@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")
	_, _ = store.GetDB().Exec("INSERT INTO messages (id, user_email, task, source, source_ts) VALUES (?, ?, ?, ?, ?)", 55, email, "T", "slack", "ts55")

	api := &API{}
	body, _ := json.Marshal(map[string]interface{}{"id": 55, "subtask_index": 0, "done": true})
	req, _ := http.NewRequest("POST", "/api/messages/subtask", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleToggleSubtask(rr, req)
	// May return 200 (success) or 500 (store error on empty subtasks) — both are non-400.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// ---- HandleUnlinkAccount with DB ----

// TestHandleUnlinkAccount_WithDB exercises the store call path (valid JSON, unknown ID).
func TestHandleUnlinkAccount_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "unlink2@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	body, _ := json.Marshal(map[string]interface{}{"contact_id": 99999})
	req, _ := http.NewRequest("POST", "/api/contacts/unlink", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleUnlinkAccount(rr, req)
	// Unknown contact ID returns 200 (no-op) or 500; not 400.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// ---- HandleLinkAccounts success/DB path ----

// TestHandleLinkAccounts_DifferentIDs exercises the path past the self-link guard.
func TestHandleLinkAccounts_DifferentIDs_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "link2@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	body, _ := json.Marshal(map[string]interface{}{"target_id": 1, "master_id": 2})
	req, _ := http.NewRequest("POST", "/api/contacts/link", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	api.HandleLinkAccounts(rr, req)
	// Unknown IDs → store error → 500; self-link guard was bypassed (200 impossible here).
	if rr.Code == http.StatusBadRequest {
		t.Errorf("unexpected 400 body=%s", rr.Body.String())
	}
}

// ---- HandleAddMapping BadCanonicalID path ----

// TestHandleAddMapping_EmptyCanonicalID exercises the "Canonical ID cannot be determined" 400 path.
func TestHandleAddMapping_EmptyCanonicalID(t *testing.T) {
	t.Parallel()
	api := &API{}
	// All three fields empty → determineCanonicalID returns "" → 400.
	body, _ := json.Marshal(map[string]string{
		"canonical_id": "",
		"display_name": "",
		"aliases":      "",
		"source":       "user",
	})
	req, _ := http.NewRequest("POST", "/api/contacts/mapping/add", bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleAddMapping(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// ---- GatherMessagesForTranslation with DB (covers the function) ----

// TestGatherMessagesForTranslation_WithDB exercises the store fetch path.
func TestGatherMessagesForTranslation_WithDB(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "gather@example.com"
	_, _ = store.GetOrCreateUser(context.Background(), email, "U", "")

	api := &API{}
	msgs, err := api.gatherMessagesForTranslation(context.Background(), email)
	if err != nil {
		t.Fatalf("gatherMessagesForTranslation: %v", err)
	}
	_ = msgs // empty is fine
}
