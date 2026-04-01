package auth

import (
	"encoding/json"
	"message-consolidator/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthMiddleware_Unauthorized(t *testing.T) {
	// Setup
	AuthDisabled = false
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/messages", nil)
	rr := httptest.NewRecorder()

	// Execute
	handler.ServeHTTP(rr, req)

	// Verify Status Code
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %v", rr.Code)
	}

	// Verify Content Type
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %v", rr.Header().Get("Content-Type"))
	}

	// Verify JSON Body
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body["error"] != "unauthorized" {
		t.Errorf("expected error 'unauthorized', got %v", body["error"])
	}
	if body["code"] != float64(401) {
		t.Errorf("expected code 401, got %v", body["code"])
	}
}

func TestSetupOAuth_RedirectURL(t *testing.T) {
	cfg := &config.Config{
		AppBaseURL: "https://example.com",
	}
	SetupOAuth(cfg)

	expected := "https://example.com/api/auth/callback"
	if GoogleOauthConfig.RedirectURL != expected {
		t.Errorf("expected RedirectURL %s, got %s", expected, GoogleOauthConfig.RedirectURL)
	}
}
