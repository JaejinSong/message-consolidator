package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/whatap/go-api/instrumentation/net/http/whataphttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// sessionMaxAge bounds both the cookie lifetime and the server-side session row.
const sessionMaxAge = 24 * time.Hour

// isProdEnv reports whether cookies should carry the Secure attribute.
func isProdEnv() bool {
	return os.Getenv("ENV") == "production" || strings.HasPrefix(appBaseURL, "https://")
}

// newSessionToken returns a 256-bit opaque, unguessable session identifier.
func newSessionToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(unauthorizedResponse)
}

type authError struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

var unauthorizedResponse = authError{Error: "unauthorized", Code: http.StatusUnauthorized}

var (
	GoogleOAuthConfig *oauth2.Config
	AuthDisabled      bool
	appBaseURL        string
)

type contextKey string

const UserEmailKey contextKey = "userEmail"

func GetUserEmail(r *http.Request) string {
	if AuthDisabled {
		//Why: Provides a static fallback user for local development environments where OAuth is unavailable or disabled.
		return os.Getenv("DEFAULT_USER_EMAIL")
	}
	email, ok := r.Context().Value(UserEmailKey).(string)
	if !ok || email == "" {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(email))
}

func SetupOAuth(cfg *config.Config) {
	AuthDisabled = cfg.AuthDisabled
	appBaseURL = cfg.AppBaseURL
	GoogleOAuthConfig = &oauth2.Config{
		// Why: Matches the redirect route with handlers/routes.go and Caddyfile to avoid 404 mismatch.
		// IMPORTANT: Ensure 'https://34.67.133.18.nip.io/auth/callback' is authorized in GCP Console.
		RedirectURL:  fmt.Sprintf("%s/auth/callback", cfg.AppBaseURL),
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}
}

func HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateStateCookie(w)
	url := GoogleOAuthConfig.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func HandleGoogleCallback(w http.ResponseWriter, r *http.Request, slackToken string, lookupUserByEmail func(string) (string, string, error)) {
	oauthState, err := r.Cookie("oauthstate")

	if err != nil {
		logger.Errorf("[AUTH] missing oauth state cookie (possible domain/HTTPS mismatch): %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	if r.FormValue("state") != oauthState.Value {
		logger.Errorf("[AUTH] invalid oauth google state: expected=%s got=%s", oauthState.Value, r.FormValue("state"))
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := GoogleOAuthConfig.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		logger.Errorf("[AUTH] code exchange failed: %v", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	// Why: Bearer header keeps the access token out of URL/logs/proxies/Referer.
	// NewRoundTrip preserves the WhaTap HTTPC step and x-whatap-mtid trace propagation.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		logger.Errorf("[AUTH] failed building userinfo request: %v", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	httpClient := &http.Client{Transport: whataphttp.NewRoundTrip(r.Context(), nil)}
	response, err := httpClient.Do(req)
	if err != nil {
		logger.Errorf("[AUTH] failed getting user info: %v", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}
	defer response.Body.Close()

	var userInfo struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(response.Body).Decode(&userInfo); err != nil {
		logger.Errorf("[AUTH] failed decoding user info: %v", err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}

	//Why: Synchronizes the Google user metadata with the local database to ensure user records stay current across logins.
	user, err := store.GetOrCreateUser(r.Context(), userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		logger.Errorf("[AUTH] failed to sync user to DB: %v", err)
	} else {
		autoLinkSlack(r.Context(), user, lookupUserByEmail)
	}

	if err := SetSessionCookie(r.Context(), w, userInfo.Email); err != nil {
		logger.Errorf("[AUTH] failed to create session for %s: %v", userInfo.Email, err)
		http.Error(w, "authentication failed", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// Why: Cross-service Slack ID resolution without creating a circular package dependency between auth/store/channels.
func autoLinkSlack(ctx context.Context, user *store.User, lookup func(string) (string, string, error)) {
	slackID, realName, err := lookup(user.Email)
	if err != nil || slackID == "" {
		return
	}
	if err := store.UpdateUserSlackID(ctx, user.Email, slackID); err != nil {
		logger.Warnf("[AUTH] UpdateUserSlackID failed for %s: %v", user.Email, err)
	}
	if err := store.AddUserAlias(ctx, user.ID, realName); err != nil {
		logger.Warnf("[AUTH] AddUserAlias failed for %s: %v", user.Email, err)
	}
	logger.Infof("[AUTH] auto-discovered Slack ID %s and aliases for %s", slackID, user.Email)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Why: Invalidate the server-side session so a captured cookie is useless after logout.
	if cookie, err := r.Cookie("session_token"); err == nil && cookie.Value != "" {
		if err := store.DeleteSession(r.Context(), cookie.Value); err != nil {
			logger.Warnf("[AUTH] failed to delete session on logout: %v", err)
		}
	}

	isProd := isProdEnv()

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "session_active",
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: false,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// Why: Generates a cryptographically secure random string for use as the OAuth2 'state' parameter to prevent CSRF attacks.
func generateStateCookie(w http.ResponseWriter) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand only fails if /dev/urandom is unavailable; treat as fatal-quality but degrade to time-seeded fallback.
		logger.Errorf("[AUTH] crypto/rand.Read failed: %v", err)
	}
	state := base64.RawURLEncoding.EncodeToString(b[:])
	isProd := isProdEnv()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauthstate",
		Value:    state,
		Expires:  time.Now().Add(20 * time.Minute),
		HttpOnly: true,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	return state
}

// SetSessionCookie mints an opaque server-side session for email and sets it as an
// HttpOnly cookie. The cookie carries only the random token; the email is resolved
// from the sessions table on each request, so the cookie cannot be forged.
func SetSessionCookie(ctx context.Context, w http.ResponseWriter, email string) error {
	token, err := newSessionToken()
	if err != nil {
		return fmt.Errorf("generate session token: %w", err)
	}
	expiresAt := time.Now().Add(sessionMaxAge)
	if err := store.CreateSession(ctx, token, email, expiresAt); err != nil {
		return fmt.Errorf("persist session: %w", err)
	}

	isProd := isProdEnv()
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	//Why: Provides a public "session active" hint for frontend logic without exposing the actual token.
	http.SetCookie(w, &http.Cookie{
		Name:     "session_active",
		Value:    "true",
		Expires:  expiresAt,
		HttpOnly: false,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// AdminMiddleware enforces administrator privilege for the wrapped handler. Wraps AuthMiddleware
// so unauthenticated requests still receive the standard 401, while authenticated non-admins
// receive 403. Super admin (jjsong@whatap.io) is always allowed; other admins are toggled via
// `users.is_admin`.
func AdminMiddleware(next http.Handler) http.Handler {
	return AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := GetUserEmail(r)
		if !store.IsAdmin(r.Context(), email) {
			logger.Warnf("[AUTH] Admin access denied for %s on %s", email, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(authError{Error: "admin only", Code: http.StatusForbidden})
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AuthDisabled {
			email := os.Getenv("DEFAULT_USER_EMAIL")
			logger.Debugf("[AUTH] AuthDisabled is true. Bypassing authentication for %s. Using default user: %s", r.URL.Path, email)

			// Why: If the parameter 'email' is already present (e.g. injected by Vite Proxy or Front-end),
			// we skip manual injection to prevent 'Double Injection' that breaks logic integrity.
			if r.URL.Query().Get("email") == "" {
				q := r.URL.Query()
				q.Set("email", email)
				r.URL.RawQuery = q.Encode()
			}

			ctx := context.WithValue(r.Context(), UserEmailKey, email)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil {
			logger.Warnf("[AUTH] Session cookie missing for path: %s", r.URL.Path)
			writeUnauthorized(w)
			return
		}

		email, err := store.GetSessionEmail(r.Context(), cookie.Value)
		if err != nil {
			logger.Warnf("[AUTH] Invalid or expired session for path %s: %v", r.URL.Path, err)
			writeUnauthorized(w)
			return
		}
		logger.Debugf("[AUTH] Valid session for %s: %s", r.URL.Path, email)

		ctx := context.WithValue(r.Context(), UserEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
