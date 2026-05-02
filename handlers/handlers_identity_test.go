package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGenerateProposals_NilResolver(t *testing.T) {
	api := &API{IdentityResolver: nil}
	req := NewMockRequest("POST", "/api/identity/proposals/generate", "u@example.com")
	rr := httptest.NewRecorder()
	api.HandleGenerateProposals(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHandleProposalJobStatus_Idle(t *testing.T) {
	// Ensure no job exists for test email.
	proposalJobsMu.Lock()
	delete(proposalJobs, "fresh@example.com")
	proposalJobsMu.Unlock()

	api := &API{}
	req := NewMockRequest("GET", "/api/identity/proposals/job-status", "fresh@example.com")
	rr := httptest.NewRecorder()
	api.HandleProposalJobStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var body proposalJob
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "idle" {
		t.Errorf("status = %q, want idle", body.Status)
	}
}

func TestHandleProposalJobStatus_Running(t *testing.T) {
	email := "running@example.com"
	proposalJobsMu.Lock()
	proposalJobs[email] = &proposalJob{Status: "running"}
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
		t.Errorf("status = %d, want 200", rr.Code)
	}
	var body proposalJob
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "running" {
		t.Errorf("status = %q, want running", body.Status)
	}
}

func TestProposalJobError(t *testing.T) {
	t.Parallel()
	import_err := httpError("boom")
	job := proposalJobError(import_err)
	if job.Status != "error" {
		t.Errorf("Status = %q, want error", job.Status)
	}
	if job.ErrMsg != "boom" {
		t.Errorf("ErrMsg = %q, want boom", job.ErrMsg)
	}
}

func TestAllPairsHandled(t *testing.T) {
	t.Parallel()
	handled := map[[2]int64]bool{
		{1, 2}: true,
		{1, 3}: true,
		{2, 3}: true,
	}
	if !allPairsHandled([]int64{1, 2, 3}, handled) {
		t.Error("all pairs handled, expected true")
	}
	if allPairsHandled([]int64{1, 2, 4}, handled) {
		t.Error("pair (2,4) not handled, expected false")
	}
	if !allPairsHandled([]int64{}, handled) {
		t.Error("empty ids, expected true")
	}
	if !allPairsHandled([]int64{1}, handled) {
		t.Error("single id, expected true (no pairs)")
	}
}
