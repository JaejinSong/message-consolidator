package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"message-consolidator/config"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestHandleInternalDigest covers all four branches of the handler.
func TestHandleInternalDigest(t *testing.T) {
	t.Run("unconfigured secret returns 403", func(t *testing.T) {
		api := &API{Config: &config.Config{InternalScanSecret: ""}}
		req, _ := http.NewRequest("POST", "/api/internal/digest", nil)
		req.Header.Set("X-Internal-Secret", "anything")
		rr := httptest.NewRecorder()
		api.HandleInternalDigest(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rr.Code)
		}
	})

	t.Run("wrong secret returns 401", func(t *testing.T) {
		api := &API{Config: &config.Config{InternalScanSecret: "correct"}}
		req, _ := http.NewRequest("POST", "/api/internal/digest", nil)
		req.Header.Set("X-Internal-Secret", "wrong")
		rr := httptest.NewRecorder()
		api.HandleInternalDigest(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rr.Code)
		}
	})

	t.Run("nil Digest returns 503", func(t *testing.T) {
		api := &API{
			Config: &config.Config{InternalScanSecret: "secret"},
			Digest: nil,
		}
		req, _ := http.NewRequest("POST", "/api/internal/digest", nil)
		req.Header.Set("X-Internal-Secret", "secret")
		rr := httptest.NewRecorder()
		api.HandleInternalDigest(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("Digest error returns 500", func(t *testing.T) {
		api := &API{
			Config: &config.Config{InternalScanSecret: "secret"},
			Digest: func(ctx context.Context) error {
				return errors.New("digest failed")
			},
		}
		req, _ := http.NewRequest("POST", "/api/internal/digest", nil)
		req.Header.Set("X-Internal-Secret", "secret")
		rr := httptest.NewRecorder()
		api.HandleInternalDigest(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rr.Code)
		}
	})

	t.Run("success returns 200 with status ok", func(t *testing.T) {
		var called atomic.Bool
		api := &API{
			Config: &config.Config{InternalScanSecret: "secret"},
			Digest: func(ctx context.Context) error {
				called.Store(true)
				return nil
			},
		}
		req, _ := http.NewRequest("POST", "/api/internal/digest", nil)
		req.Header.Set("X-Internal-Secret", "secret")
		rr := httptest.NewRecorder()
		api.HandleInternalDigest(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
		}
		if !called.Load() {
			t.Error("Digest func was not called")
		}
		var body map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if body["status"] != "ok" {
			t.Errorf("status = %q, want ok", body["status"])
		}
	})
}

// TestHandleInternalScan_SuccessPath covers the happy path missed by existing auth-only tests.
func TestHandleInternalScan_SuccessPath(t *testing.T) {
	var invoked atomic.Bool
	prev := FullScanFunc
	FullScanFunc = func() { invoked.Store(true) }
	t.Cleanup(func() { FullScanFunc = prev })

	api := &API{Config: &config.Config{InternalScanSecret: "topsecret"}}
	req, _ := http.NewRequest("POST", "/api/internal/scan", nil)
	req.Header.Set("X-Internal-Secret", "topsecret")
	rr := httptest.NewRecorder()
	api.HandleInternalScan(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 body=%s", rr.Code, rr.Body.String())
	}
	if !invoked.Load() {
		t.Error("FullScanFunc was not invoked on success path")
	}
}

// TestHandleInternalScan_NilFullScanFunc covers the branch where FullScanFunc is nil.
func TestHandleInternalScan_NilFullScanFunc(t *testing.T) {
	prev := FullScanFunc
	FullScanFunc = nil
	t.Cleanup(func() { FullScanFunc = prev })

	api := &API{Config: &config.Config{InternalScanSecret: "topsecret"}}
	req, _ := http.NewRequest("POST", "/api/internal/scan", nil)
	req.Header.Set("X-Internal-Secret", "topsecret")
	rr := httptest.NewRecorder()
	api.HandleInternalScan(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 body=%s", rr.Code, rr.Body.String())
	}
}
