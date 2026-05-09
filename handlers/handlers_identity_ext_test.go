package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleGenerateProposals_NilResolverBeforeConflict verifies that nil
// IdentityResolver returns 503, which is the guard that fires before conflict check.
func TestHandleGenerateProposals_NilResolverBeforeConflict(t *testing.T) {
	t.Parallel()
	email := "pre-conflict@example.com"
	proposalJobsMu.Lock()
	proposalJobs[email] = &proposalJob{Status: "running"}
	proposalJobsMu.Unlock()
	t.Cleanup(func() {
		proposalJobsMu.Lock()
		delete(proposalJobs, email)
		proposalJobsMu.Unlock()
	})

	api := &API{IdentityResolver: nil}
	req := NewMockRequest("POST", "/api/identity/proposals/generate", email)
	rr := httptest.NewRecorder()
	api.HandleGenerateProposals(rr, req)
	// Why: nil check precedes conflict check in production code.
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

// TestAllPairsHandled_FalseCase exercises the false path of allPairsHandled.
func TestAllPairsHandled_FalseCase(t *testing.T) {
	t.Parallel()
	ids := []int64{1, 2}
	handled := map[[2]int64]bool{}
	if allPairsHandled(ids, handled) {
		t.Error("expected false for empty handled map")
	}
}

// TestAllPairsHandled_TrueCase exercises the true path of allPairsHandled.
func TestAllPairsHandled_TrueCase(t *testing.T) {
	t.Parallel()
	ids := []int64{1, 2}
	handled := map[[2]int64]bool{
		{1, 2}: true,
	}
	if !allPairsHandled(ids, handled) {
		t.Error("expected true for fully-handled pair")
	}
}

// TestAllPairsHandled_SingleID exercises the single-element path (no pairs possible).
func TestAllPairsHandled_SingleID(t *testing.T) {
	t.Parallel()
	ids := []int64{1}
	handled := map[[2]int64]bool{}
	if !allPairsHandled(ids, handled) {
		t.Error("expected true for single-element ids (no pairs possible)")
	}
}

// TestAllPairsHandled_Empty exercises the empty-ids path.
func TestAllPairsHandled_Empty(t *testing.T) {
	t.Parallel()
	if !allPairsHandled(nil, map[[2]int64]bool{}) {
		t.Error("expected true for nil ids")
	}
}

// TestHandleAcceptProposal_BadJSON verifies bindJSON failure returns 400.
// Why: bindJSON is called before any store interaction, so no DB is needed.
func TestHandleAcceptProposal_BadJSON(t *testing.T) {
	t.Parallel()
	api := &API{}
	body := bytes.NewBufferString("{bad json")
	req, _ := http.NewRequest("POST", "/api/identity/proposals/accept", body)
	req = req.WithContext(WithMockUser(req.Context(), "u@x.io"))
	rr := httptest.NewRecorder()
	api.HandleAcceptProposal(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 body=%s", rr.Code, rr.Body.String())
	}
}

// TestHandleProposalJobStatus_Done covers the done status path.
func TestHandleProposalJobStatus_Done(t *testing.T) {
	email := "done-job@example.com"
	proposalJobsMu.Lock()
	proposalJobs[email] = &proposalJob{Status: "done", Count: 5}
	proposalJobsMu.Unlock()
	t.Cleanup(func() {
		proposalJobsMu.Lock()
		delete(proposalJobs, email)
		proposalJobsMu.Unlock()
	})

	api := &API{}
	req := NewMockRequest("GET", "/api/identity/proposals/job-status", email)
	rr := httptest.NewRecorder()
	api.HandleProposalJobStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body proposalJob
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "done" {
		t.Errorf("status = %q, want done", body.Status)
	}
	if body.Count != 5 {
		t.Errorf("count = %d, want 5", body.Count)
	}
}
