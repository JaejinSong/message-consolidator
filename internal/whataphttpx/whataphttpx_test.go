package whataphttpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type capturingTransport struct {
	captured *http.Request
}

func (c *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.captured = req
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
}

func TestAPIKeyTransport_InjectsHeader(t *testing.T) {
	cap := &capturingTransport{}
	rt := &apiKeyTransport{key: "secret-key", rt: cap}

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/models", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}

	if got := cap.captured.Header.Get("x-goog-api-key"); got != "secret-key" {
		t.Errorf("x-goog-api-key header = %q, want %q", got, "secret-key")
	}
}

func TestAPIKeyTransport_DoesNotMutateOriginalRequest(t *testing.T) {
	rt := &apiKeyTransport{key: "secret-key", rt: &capturingTransport{}}

	req := httptest.NewRequest(http.MethodPost, "https://example.com/v1/models", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}

	if req.Header.Get("x-goog-api-key") != "" {
		t.Error("RoundTrip mutated original request headers")
	}
}
