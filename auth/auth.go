package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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

type authError struct {
	Error string `json:"error"`
	Code  int    `json:"code"`
}

var unauthorizedResponse = authError{Error: "unauthorized", Code: http.StatusUnauthorized}

var (
	GoogleOauthConfig *oauth2.Config
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
	GoogleOauthConfig = &oauth2.Config{
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
	url := GoogleOauthConfig.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func HandleGoogleCallback(w http.ResponseWriter, r *http.Request, slackToken string, lookupUserByEmail func(string) (string, string, error)) {
	oauthState, err := r.Cookie("oauthstate")

	if err != nil {
		logger.Errorf("Missing oauth state cookie (Why: Possible Domain/HTTPS mismatch?): %v", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	if r.FormValue("state") != oauthState.Value {
		logger.Errorf("Invalid oauth google state. Expected: %s, Got: %s", oauthState.Value, r.FormValue("state"))
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := GoogleOauthConfig.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		fmt.Fprintf(w, "Code exchange failed: %s", err.Error())
		return
	}

	// Why: whataphttp.HttpGet wraps the call as a WhaTap HTTPC step on the active transaction
	// and propagates trace context (x-whatap-mtid headers) for distributed tracing.
	response, err := whataphttp.HttpGet(r.Context(), "https://www.googleapis.com/oauth2/v2/userinfo?access_token="+token.AccessToken)
	if err != nil {
		fmt.Fprintf(w, "Failed getting user info: %s", err.Error())
		return
	}
	defer response.Body.Close()

	var userInfo struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(response.Body).Decode(&userInfo); err != nil {
		fmt.Fprintf(w, "Failed decoding user info: %s", err.Error())
		return
	}

	//Why: Synchronizes the Google user metadata with the local database to ensure user records stay current across logins.
	user, err := store.GetOrCreateUser(r.Context(), userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		logger.Errorf("Failed to sync user to DB: %v", err)
	} else {
		autoLinkSlack(r.Context(), user, lookupUserByEmail)
	}

	SetSessionCookie(w, userInfo.Email)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

//Why: Cross-service Slack ID resolution without creating a circular package dependency between auth/store/channels.
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
	logger.Infof("Auto-discovered Slack ID %s and aliases for %s", slackID, user.Email)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	isProd := os.Getenv("ENV") == "production" || strings.HasPrefix(appBaseURL, "https://")
	
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

//Why: Generates a cryptographically secure random string for use as the OAuth2 'state' parameter to prevent CSRF attacks.
func generateStateCookie(w http.ResponseWriter) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand only fails if /dev/urandom is unavailable; treat as fatal-quality but degrade to time-seeded fallback.
		logger.Errorf("[AUTH] crypto/rand.Read failed: %v", err)
	}
	state := base64.RawURLEncoding.EncodeToString(b[:])
	isProd := os.Getenv("ENV") == "production" || strings.HasPrefix(appBaseURL, "https://")
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

func SetSessionCookie(w http.ResponseWriter, email string) {
	isProd := os.Getenv("ENV") == "production" || strings.HasPrefix(appBaseURL, "https://")
	maxAge := 24 * time.Hour

	//Why: Establishes a server-side session using an HttpOnly cookie. Lax mode is used for better balance between security and cross-site functionality in proxied environments.
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    base64.RawURLEncoding.EncodeToString([]byte(email)),
		Expires:  time.Now().Add(maxAge),
		HttpOnly: true,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})

	//Why: Provides a public "session active" hint for frontend logic without exposing the actual token.
	http.SetCookie(w, &http.Cookie{
		Name:     "session_active",
		Value:    "true",
		Expires:  time.Now().Add(maxAge),
		HttpOnly: false,
		Secure:   isProd,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(unauthorizedResponse)
			return
		}

		decodedEmailBytes, err := base64.RawURLEncoding.DecodeString(cookie.Value)
		if err != nil {
			decodedEmailBytes, err = base64.URLEncoding.DecodeString(cookie.Value)
		}
		if err != nil {
			logger.Errorf("[AUTH] Error decoding session cookie for %s: %v", r.URL.Path, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(unauthorizedResponse)
			return
		}
		email := string(decodedEmailBytes)
		logger.Debugf("[AUTH] Valid session for %s: %s", r.URL.Path, email)

		ctx := context.WithValue(r.Context(), UserEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
