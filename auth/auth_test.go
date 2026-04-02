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

	expected := "https://example.com/auth/callback"
	if GoogleOauthConfig.RedirectURL != expected {
		t.Errorf("expected RedirectURL %s, got %s", expected, GoogleOauthConfig.RedirectURL)
	}
}

func TestSetSessionCookie_Attributes(t *testing.T) {
	tests := []struct {
		name       string
		appBaseURL string
		env        string
		wantSecure bool
	}{
		{
			name:       "Production with HTTPS",
			appBaseURL: "https://example.com",
			env:        "production",
			wantSecure: true,
		},
		{
			name:       "Development with HTTP",
			appBaseURL: "http://localhost:8080",
			env:        "development",
			wantSecure: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			appBaseURL = tt.appBaseURL
			t.Setenv("ENV", tt.env)
			rr := httptest.NewRecorder()

			// Execute
			SetSessionCookie(rr, "test@example.com")

			// Verify
			cookies := rr.Result().Cookies()
			var sessionCookie *http.Cookie
			for _, c := range cookies {
				if c.Name == "session_token" {
					sessionCookie = c
					break
				}
			}

			if sessionCookie == nil {
				t.Fatal("session_token cookie not found")
			}

			if sessionCookie.Secure != tt.wantSecure {
				t.Errorf("expected Secure %v, got %v", tt.wantSecure, sessionCookie.Secure)
			}

			if sessionCookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("expected SameSite Lax, got %v", sessionCookie.SameSite)
			}

			if !sessionCookie.HttpOnly {
				t.Error("expected HttpOnly to be true")
			}
		})
	}
}
