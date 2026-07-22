package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"message-consolidator/internal/testutil"
	"message-consolidator/store"
)

// seedExclusionTask inserts an active TASK row and returns its ID.
func seedExclusionTask(t *testing.T, email string) store.MessageID {
	t.Helper()
	res, err := store.GetDB().Exec(
		`INSERT INTO messages (user_email, task, source, source_ts, done, is_deleted, category)
		 VALUES (?, 'Stale task', 'slack', ?, 0, 0, 'TASK')`,
		email, testutil.RandomTS("excl"))
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id, _ := res.LastInsertId()
	return store.MessageID(id)
}

func exclusionRowLifecycle(t *testing.T, id store.MessageID) (string, bool) {
	t.Helper()
	var lifecycle string
	var excludedAt *string
	err := store.GetDB().QueryRow(`SELECT lifecycle, excluded_at FROM messages WHERE id = ?`, int64(id)).
		Scan(&lifecycle, &excludedAt)
	if err != nil {
		t.Fatalf("row state: %v", err)
	}
	return lifecycle, excludedAt != nil
}

func postExclusion(t *testing.T, handler http.HandlerFunc, url, email string, id store.MessageID) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req = req.WithContext(WithMockUser(req.Context(), email))
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestHandleConfirmExclusion(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("confirm-excl")
	_, _ = store.GetOrCreateUser(context.Background(), email, "Excl User", "")
	id := seedExclusionTask(t, email)

	api := &API{}
	rr := postExclusion(t, api.HandleConfirmExclusion, "/api/messages/exclusion-candidate/confirm", email, id)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	// respondWithUpdatedUser contract: {"user": {...}} for immediate state propagation.
	var resp struct {
		User *store.User `json:"user"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.User == nil {
		t.Fatalf("expected user envelope, got %s (err=%v)", rr.Body.String(), err)
	}
	if lifecycle, hasExcludedAt := exclusionRowLifecycle(t, id); lifecycle != "excluded" || !hasExcludedAt {
		t.Errorf("lifecycle=%s hasExcludedAt=%v, want excluded/true", lifecycle, hasExcludedAt)
	}
}

func TestHandleConfirmExclusion_DoneTask404(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("confirm-done")
	_, _ = store.GetOrCreateUser(context.Background(), email, "Done User", "")
	id := seedExclusionTask(t, email)
	if _, err := store.GetDB().Exec(`UPDATE messages SET done = 1 WHERE id = ?`, int64(id)); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	api := &API{}
	rr := postExclusion(t, api.HandleConfirmExclusion, "/api/messages/exclusion-candidate/confirm", email, id)

	// Store returns sql.ErrNoRows for terminal states so the handler can 404.
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rr.Code, rr.Body.String())
	}
	if lifecycle, _ := exclusionRowLifecycle(t, id); lifecycle != "done" {
		t.Errorf("lifecycle=%s, want done (unchanged)", lifecycle)
	}
}

func TestHandleDismissExclusionCandidate(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("dismiss-excl")
	_, _ = store.GetOrCreateUser(context.Background(), email, "Dismiss User", "")
	id := seedExclusionTask(t, email)
	cand := store.ExclusionCandidate{ProposedAt: "2026-06-20T00:00:00Z", DaysStalled: 31, Status: "pending"}
	if err := store.ProposeExclusionCandidate(context.Background(), store.GetDB(), email, id, cand); err != nil {
		t.Fatalf("propose: %v", err)
	}

	api := &API{}
	rr := postExclusion(t, api.HandleDismissExclusionCandidate, "/api/messages/exclusion-candidate/dismiss", email, id)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var metadata string
	if err := store.GetDB().QueryRow(`SELECT COALESCE(metadata, '') FROM messages WHERE id = ?`, int64(id)).Scan(&metadata); err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if store.HasExclusionCandidate(metadata) {
		t.Error("candidate must be removed after dismiss")
	}
	if _, ok := store.ExclusionDismissedAt(metadata); !ok {
		t.Error("dismissed_at must be stamped after dismiss")
	}
	if lifecycle, _ := exclusionRowLifecycle(t, id); lifecycle != "active" {
		t.Errorf("lifecycle=%s, want active (dismiss keeps task tracked)", lifecycle)
	}
}

func TestHandleRestoreExcluded(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("restore-excl")
	_, _ = store.GetOrCreateUser(context.Background(), email, "Restore User", "")
	id := seedExclusionTask(t, email)
	if err := store.ConfirmExclusion(context.Background(), store.GetDB(), email, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	api := &API{}
	rr := postExclusion(t, api.HandleRestoreExcluded, "/api/messages/excluded/restore", email, id)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		User *store.User `json:"user"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil || resp.User == nil {
		t.Fatalf("expected user envelope, got %s (err=%v)", rr.Body.String(), err)
	}
	if lifecycle, hasExcludedAt := exclusionRowLifecycle(t, id); lifecycle != "active" || hasExcludedAt {
		t.Errorf("lifecycle=%s hasExcludedAt=%v, want active/false", lifecycle, hasExcludedAt)
	}
}

func TestHandleRestoreExcluded_NotExcluded404(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("restore-active")
	_, _ = store.GetOrCreateUser(context.Background(), email, "Active User", "")
	id := seedExclusionTask(t, email) // active, never excluded

	api := &API{}
	rr := postExclusion(t, api.HandleRestoreExcluded, "/api/messages/excluded/restore", email, id)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rr.Code, rr.Body.String())
	}
}

func TestExclusionHandlers_InvalidID(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer cleanup()

	email := testutil.RandomEmail("invalid-id")
	api := &API{}
	handlers := map[string]http.HandlerFunc{
		"confirm": api.HandleConfirmExclusion,
		"dismiss": api.HandleDismissExclusionCandidate,
		"restore": api.HandleRestoreExcluded,
	}
	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			rr := postExclusion(t, h, fmt.Sprintf("/api/test/%s", name), email, 0)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rr.Code)
			}
		})
	}
}
