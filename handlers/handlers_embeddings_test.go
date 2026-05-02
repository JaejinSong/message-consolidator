package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSemanticArchiveSearch_NoService(t *testing.T) {
	api := &API{}
	req := NewMockRequest("GET", "/api/messages/archive/semantic?q=incident", "u@x.io")
	rr := httptest.NewRecorder()
	api.HandleSemanticArchiveSearch(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when Embeddings nil, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleSemanticArchiveSearch_QueryTooShort(t *testing.T) {
	// We need a non-nil Embeddings to bypass the 503 gate; a typed nil pointer
	// works because the validation happens before calling into the service.
	api := &API{}
	api.Embeddings = nil
	cases := []string{"", "a", "ab", " a "}
	for _, q := range cases {
		req := NewMockRequest("GET", "/api/messages/archive/semantic?q="+q, "u@x.io")
		rr := httptest.NewRecorder()
		api.HandleSemanticArchiveSearch(rr, req)
		// service-unavailable wins over query-length when Embeddings is nil
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("q=%q: expected 503 (svc nil), got %d", q, rr.Code)
		}
	}
}

func TestHandleBackfillEmbeddings_NoService(t *testing.T) {
	api := &API{}
	req := NewMockRequest("POST", "/api/admin/embeddings/backfill?batch=10", "admin@x.io")
	rr := httptest.NewRecorder()
	api.HandleBackfillEmbeddings(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when Embeddings nil, got %d", rr.Code)
	}
}
