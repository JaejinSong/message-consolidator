package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"message-consolidator/config"
	"message-consolidator/internal/testutil"
	"message-consolidator/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthMiddleware_Unauthorized(t *testing.T) {
	AuthDisabled = false
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/messages", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %v", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %v", rr.Header().Get("Content-Type"))
	}
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

// TestAuthMiddleware_UnauthorizedClearsSessionHint verifies a 401 expires the
// public session_active hint so a stale hint cannot suppress the login overlay.
func TestAuthMiddleware_UnauthorizedClearsSessionHint(t *testing.T) {
	AuthDisabled = false
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/messages", nil)
	req.AddCookie(&http.Cookie{Name: "session_active", Value: "true"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %v", rr.Code)
	}
	cleared := false
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session_active" && c.Value == "" && c.Expires.Before(time.Now()) {
			cleared = true
		}
	}
	if !cleared {
		t.Error("401 response must expire the session_active hint cookie")
	}
}

func TestGetUserEmail_AuthDisabled(t *testing.T) {
	AuthDisabled = true
	t.Setenv("DEFAULT_USER_EMAIL", "dev@example.com")
	req := httptest.NewRequest("GET", "/", nil)
	if got := GetUserEmail(req); got != "dev@example.com" {
		t.Errorf("GetUserEmail = %q, want dev@example.com", got)
	}
	AuthDisabled = false
}

func TestGetUserEmail_FromContext(t *testing.T) {
	AuthDisabled = false
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), UserEmailKey, "Alice@Example.COM")
	if got := GetUserEmail(req.WithContext(ctx)); got != "alice@example.com" {
		t.Errorf("GetUserEmail = %q, want alice@example.com", got)
	}
}

func TestGetUserEmail_MissingContext(t *testing.T) {
	AuthDisabled = false
	req := httptest.NewRequest("GET", "/", nil)
	if got := GetUserEmail(req); got != "" {
		t.Errorf("GetUserEmail = %q, want empty", got)
	}
}

func TestHandleLogout_ClearsSessionCookies(t *testing.T) {
	appBaseURL = "http://localhost"
	req := httptest.NewRequest("GET", "/auth/logout", nil)
	rr := httptest.NewRecorder()
	HandleLogout(rr, req)
	if rr.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want 307", rr.Code)
	}
	found := map[string]bool{}
	for _, c := range rr.Result().Cookies() {
		found[c.Name] = true
	}
	if !found["session_token"] {
		t.Error("session_token cookie not cleared")
	}
	if !found["session_active"] {
		t.Error("session_active cookie not cleared")
	}
}

func TestAuthMiddleware_ValidCookie(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	AuthDisabled = false
	token := "valid-token-abc"
	if err := store.CreateSession(context.Background(), token, "user@example.com", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rr := httptest.NewRecorder()
	var gotEmail string
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = GetUserEmail(r)
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotEmail != "user@example.com" {
		t.Errorf("email = %q, want user@example.com", gotEmail)
	}
}

func TestAuthMiddleware_UnknownToken(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	AuthDisabled = false
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "no-such-token"})
	rr := httptest.NewRecorder()
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be reached")
	}))
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

// TestAuthMiddleware_ForgedBase64EmailRejected locks in the CRITICAL fix: a cookie
// that is merely base64(email) — the old, forgeable token format — must NOT authenticate,
// even for the super admin address.
func TestAuthMiddleware_ForgedBase64EmailRejected(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	AuthDisabled = false
	forged := base64.RawURLEncoding.EncodeToString([]byte(store.SuperAdminEmail))
	req := httptest.NewRequest("GET", "/api/admin/x", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: forged})
	rr := httptest.NewRecorder()
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("forged base64 email cookie must not authenticate")
	}))
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddleware_ExpiredSessionRejected(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	AuthDisabled = false
	token := "expired-token"
	if err := store.CreateSession(context.Background(), token, "user@example.com", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rr := httptest.NewRecorder()
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("expired session must not authenticate")
	}))
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddleware_AuthDisabled(t *testing.T) {
	AuthDisabled = true
	t.Setenv("DEFAULT_USER_EMAIL", "bypass@example.com")
	req := httptest.NewRequest("GET", "/api/x", nil)
	rr := httptest.NewRecorder()
	var gotEmail string
	handler := AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEmail = GetUserEmail(r)
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)
	AuthDisabled = false
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if gotEmail != "bypass@example.com" {
		t.Errorf("email = %q, want bypass@example.com", gotEmail)
	}
}

func TestGenerateStateCookie(t *testing.T) {
	appBaseURL = "http://localhost"
	rr := httptest.NewRecorder()
	state := generateStateCookie(rr)
	if state == "" {
		t.Error("state should not be empty")
	}
	var found bool
	for _, c := range rr.Result().Cookies() {
		if c.Name == "oauthstate" && c.Value == state {
			found = true
		}
	}
	if !found {
		t.Error("oauthstate cookie not set")
	}
}

func TestSetupOAuth_RedirectURL(t *testing.T) {
	cfg := &config.Config{
		AppBaseURL: "https://example.com",
	}
	SetupOAuth(cfg)

	expected := "https://example.com/auth/callback"
	if GoogleOAuthConfig.RedirectURL != expected {
		t.Errorf("expected RedirectURL %s, got %s", expected, GoogleOAuthConfig.RedirectURL)
	}
}

func TestSetSessionCookie_Attributes(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	tests := []struct {
		name       string
		appBaseURL string
		env        string
		wantSecure bool
	}{
		{"Production with HTTPS", "https://example.com", "production", true},
		{"Development with HTTP", "http://localhost:8080", "development", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appBaseURL = tt.appBaseURL
			t.Setenv("ENV", tt.env)
			rr := httptest.NewRecorder()
			if err := SetSessionCookie(context.Background(), rr, "test@example.com"); err != nil {
				t.Fatalf("SetSessionCookie: %v", err)
			}

			var sessionCookie *http.Cookie
			for _, c := range rr.Result().Cookies() {
				if c.Name == "session_token" {
					sessionCookie = c
					break
				}
			}
			if sessionCookie == nil {
				t.Fatal("session_token cookie not found")
			}
			if sessionCookie.Secure != tt.wantSecure {
				t.Errorf("Secure = %v, want %v", sessionCookie.Secure, tt.wantSecure)
			}
			if sessionCookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", sessionCookie.SameSite)
			}
			if !sessionCookie.HttpOnly {
				t.Error("expected HttpOnly=true")
			}
			// Cookie value must be an opaque token, never the email or its base64.
			if sessionCookie.Value == "test@example.com" ||
				sessionCookie.Value == base64.RawURLEncoding.EncodeToString([]byte("test@example.com")) {
				t.Error("cookie value leaks the email; must be an opaque token")
			}
			// The token must resolve back to the email via the server-side store.
			email, err := store.GetSessionEmail(context.Background(), sessionCookie.Value)
			if err != nil || email != "test@example.com" {
				t.Errorf("GetSessionEmail = (%q, %v), want test@example.com", email, err)
			}
		})
	}
}

// TestHandleLogout_InvalidatesSession verifies logout deletes the server-side row so a
// captured cookie cannot be replayed.
func TestHandleLogout_InvalidatesSession(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	appBaseURL = "http://localhost"
	token := "logout-token"
	if err := store.CreateSession(context.Background(), token, "user@example.com", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := httptest.NewRequest("GET", "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	HandleLogout(httptest.NewRecorder(), req)

	if _, err := store.GetSessionEmail(context.Background(), token); err == nil {
		t.Error("session should be invalid after logout")
	}
}

func TestHandleGoogleLogin_Redirects(t *testing.T) {
	SetupOAuth(&config.Config{
		AppBaseURL:         "http://localhost",
		GoogleClientID:     "test-id",
		GoogleClientSecret: "test-secret",
	})
	appBaseURL = "http://localhost"
	req := httptest.NewRequest("GET", "/auth/login", nil)
	rr := httptest.NewRecorder()
	HandleGoogleLogin(rr, req)
	if rr.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want 307", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if loc == "" {
		t.Error("Location header should not be empty")
	}
}

func TestHandleLogout_ProdSecureCookie(t *testing.T) {
	appBaseURL = "https://prod.example.com"
	req := httptest.NewRequest("GET", "/auth/logout", nil)
	rr := httptest.NewRecorder()
	HandleLogout(rr, req)
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session_token" && !c.Secure {
			t.Error("session_token cookie should be Secure in prod")
		}
	}
	// Reset
	appBaseURL = "http://localhost"
	_ = time.Now() // ensure time import used
}

func TestAdminMiddleware_NonAdmin(t *testing.T) {
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	defer cleanup()

	AuthDisabled = false
	token := "admin-test-token"
	if err := store.CreateSession(context.Background(), token, "regular@example.com", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})

	rr := httptest.NewRecorder()
	handler := AdminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for non-admin")
	}))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "admin only" {
		t.Errorf("error = %v, want admin only", body["error"])
	}
}
