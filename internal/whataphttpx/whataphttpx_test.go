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

func TestClient_ReturnsConfiguredHTTPClient(t *testing.T) {
	c := Client()
	if c == nil {
		t.Fatal("Client() returned nil")
	}
	if c.Transport == nil {
		t.Fatal("Client() Transport is nil — WhaTap RoundTripper missing")
	}
}

func TestWrapClient_PreservesAndWrapsTransport(t *testing.T) {
	base := &capturingTransport{}
	c := &http.Client{Transport: base}
	got := WrapClient(c)
	if got != c {
		t.Errorf("WrapClient should return same client (got %p, want %p)", got, c)
	}
	if c.Transport == nil {
		t.Fatal("Transport is nil after WrapClient")
	}
	// Why: post-wrap Transport must differ from the original — otherwise the
	// WhaTap RoundTripper was not installed on top of the OAuth transport.
	if any(c.Transport) == any(base) {
		t.Error("WrapClient did not wrap the original transport")
	}
}

func TestClientWithAPIKey_BuildsFunctionalRoundTrip(t *testing.T) {
	c := ClientWithAPIKey("my-key")
	if c == nil || c.Transport == nil {
		t.Fatal("ClientWithAPIKey returned nil/incomplete client")
	}
}
