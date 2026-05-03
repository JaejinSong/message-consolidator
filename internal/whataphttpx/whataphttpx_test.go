package whataphttpx

import (
	"net/http"
	"testing"
)

type capturingTransport struct {
	captured *http.Request
}

func (c *capturingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.captured = req
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
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
