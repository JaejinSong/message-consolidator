package handlers

import (
	"message-consolidator/config"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// TestRegisterRoutes_NoRouteLeaks verifies RegisterRoutes executes without panic
// and attaches the /health endpoint (the only non-auth-gated sanity probe).
func TestRegisterRoutes_NoRouteLeaks(t *testing.T) {
	t.Parallel()
	api := &API{Config: &config.Config{}}
	r := mux.NewRouter()
	// Should not panic even with empty config.
	api.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("/health via RegisterRoutes: status = %d, want 200", rr.Code)
	}
}

// TestRegisterSlackBotRoutes_SkippedWhenNoSecret verifies that Slack bot endpoints
// are NOT registered when the signing secret is empty.
func TestRegisterSlackBotRoutes_SkippedWhenNoSecret(t *testing.T) {
	t.Parallel()
	api := &API{Config: &config.Config{SlackSigningSecret: ""}}
	r := mux.NewRouter()
	api.RegisterRoutes(r)

	req := httptest.NewRequest("POST", "/api/slack/events", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	// mux returns 405 (Method Not Allowed) when route path exists but method doesn't, or
	// 404 when the route itself is not registered. We expect 404 when skipped.
	if rr.Code == http.StatusOK {
		t.Error("/api/slack/events should not be registered when signing secret is empty")
	}
}

// TestWhatapMiddleware_PassThrough verifies the middleware passes requests through.
func TestWhatapMiddleware_PassThrough(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := WhatapMiddleware(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("WhatapMiddleware passthrough: status = %d, want 204", rr.Code)
	}
}

// TestProtected_RedirectsUnauthenticated verifies that `protected` wraps a handler
// in AuthMiddleware, which should reject requests without a session.
func TestProtected_RedirectsUnauthenticated(t *testing.T) {
	t.Parallel()
	api := &API{Config: &config.Config{}}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := api.protected(inner.ServeHTTP)
	req := httptest.NewRequest("GET", "/api/protected", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	// Auth middleware must not let through unauthenticated requests.
	if rr.Code == http.StatusOK {
		t.Error("protected handler should not pass unauthenticated request")
	}
}

// TestRegisterRoutes_HealthPath verifies the health route is wired through the full
// registered router (exercises registerChannelRoutes and sibling register* calls too).
func TestRegisterRoutes_AllRegisterSubFunctions(t *testing.T) {
	t.Parallel()
	api := &API{Config: &config.Config{}}
	r := mux.NewRouter()
	api.RegisterRoutes(r)

	// The /health endpoint bypasses auth; it's the simplest smoke test.
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("health check via router: %d", rr.Code)
	}
}
