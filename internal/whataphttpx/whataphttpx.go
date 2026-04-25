// Package whataphttpx centralises the WhaTap HTTP RoundTripper wiring so that
// every outbound HTTP integration (Notion, Slack, Gmail, Gemini, ...) reports
// to WhaTap as an HTTPC step on the active transaction without each call site
// re-importing the whataphttp package.
//
// Per-request trace context propagates via http.Request.Context(); the ctx
// passed to whataphttp.NewRoundTrip is only a baseline fallback, so all helper
// functions construct the round tripper with context.Background() and rely on
// the request context for live trace propagation.
package whataphttpx

import (
	"context"
	"net/http"

	"github.com/whatap/go-api/instrumentation/net/http/whataphttp"
)

// Client returns a fresh *http.Client whose Transport reports to WhaTap.
// Use for SDKs that accept a plain *http.Client (slack-go, custom REST).
func Client() *http.Client {
	return &http.Client{Transport: whataphttp.NewRoundTrip(context.Background(), nil)}
}

// WrapClient mutates c.Transport so that an existing client (typically the one
// returned from oauth2.NewClient) reports its outbound calls to WhaTap. The
// caller's original transport is preserved as the wrapper's base, so OAuth2
// token injection still happens before the WhaTap RoundTripper observes the
// request. Returns c to enable inline use at the call site.
func WrapClient(c *http.Client) *http.Client {
	c.Transport = whataphttp.NewRoundTrip(context.Background(), c.Transport)
	return c
}

// ClientWithAPIKey returns an *http.Client that injects the Gemini API key as
// the x-goog-api-key header before the WhaTap RoundTripper observes the
// request.
//
// Why: google.golang.org/api/option.WithHTTPClient causes the API library to
// skip option.WithAPIKey, so the custom client must inject auth itself —
// mirror of how WrapClient preserves OAuth2 token injection beneath WhaTap.
func ClientWithAPIKey(apiKey string) *http.Client {
	base := &apiKeyTransport{key: apiKey, rt: http.DefaultTransport}
	return &http.Client{Transport: whataphttp.NewRoundTrip(context.Background(), base)}
}

type apiKeyTransport struct {
	key string
	rt  http.RoundTripper
}

func (t *apiKeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("x-goog-api-key", t.key)
	return t.rt.RoundTrip(r)
}
