package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleHealth(t *testing.T) {
	api := &API{}
	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()

	api.HandleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["status"] != "OK" {
		t.Errorf("expected status=OK, got %q", body["status"])
	}
}
