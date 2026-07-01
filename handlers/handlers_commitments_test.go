package handlers

import (
	"encoding/json"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// seedCommitment inserts a PROMISE/WAITING row. assigneeCanon/requesterCanon are stored in
// assignee/requester columns; the v_messages view resolves them as canonical IDs when no
// contact entry exists (fallback: COALESCE(canonical_id, raw_value)).
func seedCommitment(t *testing.T, email, task, category, assigneeCanon, requesterCanon string) {
	t.Helper()
	src := testutil.RandomTS("cmt")
	_, err := store.GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  assignee, requester)
		 VALUES (?, ?, ?, 'slack', 'general', ?, 0, 0, '{}', ?, ?)`,
		email, task, category, src, assigneeCanon, requesterCanon,
	)
	if err != nil {
		t.Fatalf("seedCommitment: %v", err)
	}
}

func seedCommitmentWithDeadline(t *testing.T, email, task, category, assigneeCanon, requesterCanon, deadlineDate string, past bool) {
	t.Helper()
	src := testutil.RandomTS("cmtd")
	ddDate := time.Now().UTC().AddDate(0, 0, 3).Format("2006-01-02")
	if past {
		ddDate = time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	}
	if deadlineDate != "" {
		ddDate = deadlineDate
	}
	_, err := store.GetDB().Exec(
		`INSERT INTO messages
		 (user_email, task, category, source, room, source_ts, done, is_deleted, metadata,
		  assignee, requester, deadline_date)
		 VALUES (?, ?, ?, 'slack', 'general', ?, 0, 0, '{}', ?, ?, ?)`,
		email, task, category, src, assigneeCanon, requesterCanon, ddDate,
	)
	if err != nil {
		t.Fatalf("seedCommitmentWithDeadline: %v", err)
	}
}

func TestHandleGetCommitments_DefaultViewMine(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "alice@test.com"
	seedCommitment(t, email, "Write report", "PROMISE", email, "bob")
	seedCommitment(t, email, "Get design", "WAITING", "carol", email)

	api := &API{}
	req := NewMockRequest("GET", "/api/commitments", email)
	rr := httptest.NewRecorder()

	api.HandleGetCommitments(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp CommitmentsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Default view=mine → only PROMISE rows
	total := len(resp.Overdue) + len(resp.Undated) + len(resp.Active)
	if total != 1 {
		t.Errorf("expected 1 PROMISE item in default (mine) view, got %d", total)
	}
}

func TestHandleGetCommitments_ViewWaiting(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "bob@test.com"
	seedCommitment(t, email, "Write report", "PROMISE", email, "carol")
	seedCommitment(t, email, "Get design", "WAITING", "dave", email)

	api := &API{}
	req := NewMockRequest("GET", "/api/commitments?view=waiting", email)
	rr := httptest.NewRecorder()

	api.HandleGetCommitments(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp CommitmentsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	total := len(resp.Overdue) + len(resp.Undated) + len(resp.Active)
	if total != 1 {
		t.Errorf("expected 1 WAITING item, got %d", total)
	}
}

func TestHandleGetCommitments_OverdueBucket(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "carol@test.com"
	seedCommitmentWithDeadline(t, email, "Overdue task", "PROMISE", email, "dave", "", true)

	api := &API{}
	req := NewMockRequest("GET", "/api/commitments", email)
	rr := httptest.NewRecorder()

	api.HandleGetCommitments(rr, req)

	var resp CommitmentsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	if len(resp.Overdue) != 1 {
		t.Errorf("expected 1 overdue item, got %d", len(resp.Overdue))
	}
}

func TestHandleGetCommitments_ExcludesDeleted(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	email := "erin@test.com"
	seedCommitment(t, email, "Soft deleted promise", "PROMISE", email, "bob")
	if _, err := store.GetDB().Exec(
		`UPDATE messages SET is_deleted = 1 WHERE user_email = ?`, email,
	); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	api := &API{}
	req := NewMockRequest("GET", "/api/commitments", email)
	rr := httptest.NewRecorder()

	api.HandleGetCommitments(rr, req)

	var resp CommitmentsResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	total := len(resp.Overdue) + len(resp.Undated) + len(resp.Active)
	if total != 0 {
		t.Errorf("expected soft-deleted commitment to be excluded, got %d items", total)
	}
}

func TestCommitmentMessageIDs_Dedup(t *testing.T) {
	resp := &CommitmentsResponse{
		Overdue: []CommitmentItem{{ID: 1}, {ID: 2}},
		Undated: []CommitmentItem{{ID: 2}}, // dup across buckets
		Active:  []CommitmentItem{{ID: 0}}, // zero id skipped
		Stalled: StalledBucketsResponse{
			Mine:     []StalledItem{{ID: 3}},
			Observed: []StalledItem{{ID: 1}}, // dup with overdue
		},
	}

	ids := commitmentMessageIDs(resp)

	if len(ids) != 3 {
		t.Fatalf("expected 3 unique non-zero ids, got %d: %v", len(ids), ids)
	}
	want := map[store.MessageID]bool{1: true, 2: true, 3: true}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("unexpected id %d", id)
		}
	}
}

func TestHandleGetCommitments_Unauthorized(t *testing.T) {
	api := &API{}
	req, _ := http.NewRequest("GET", "/api/commitments", nil)
	rr := httptest.NewRecorder()

	api.HandleGetCommitments(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}
