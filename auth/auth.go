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
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	GoogleOauthConfig *oauth2.Config
	AuthDisabled      bool
)

type contextKey string

const UserEmailKey contextKey = "userEmail"

func GetUserEmail(r *http.Request) string {
	if AuthDisabled {
		return "jjsong@whatap.io" // Default user ONLY when auth is strictly disabled for dev
	}
	email, ok := r.Context().Value(UserEmailKey).(string)
	if !ok || email == "" {
		return ""
	}
	return strings.TrimSpace(strings.ToLower(email))
}

func SetupOAuth(cfg *config.Config) {
	AuthDisabled = cfg.AuthDisabled
	GoogleOauthConfig = &oauth2.Config{
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
	oauthState, _ := r.Cookie("oauthstate")

	if r.FormValue("state") != oauthState.Value {
		logger.Errorf("Invalid oauth google state")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := GoogleOauthConfig.Exchange(r.Context(), r.FormValue("code"))
	if err != nil {
		fmt.Fprintf(w, "Code exchange failed: %s", err.Error())
		return
	}

	response, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + token.AccessToken)
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

	// Create or Update user in DB
	user, err := store.GetOrCreateUser(userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		logger.Errorf("Failed to sync user to DB: %v", err)
	} else {
		// Use the callback for Slack lookup to avoid dependency on main
		slackID, realName, err := lookupUserByEmail(user.Email)
		if err == nil && slackID != "" {
			store.UpdateUserSlackID(user.Email, slackID)
			store.AddUserAlias(user.ID, realName)
			logger.Infof("Auto-discovered Slack ID %s and aliases for %s", slackID, user.Email)
		}
	}

	SetSessionCookie(w, userInfo.Email)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie := http.Cookie{
		Name:     "session_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   false, // Allow local development
		Path:     "/",
	}
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func generateStateCookie(w http.ResponseWriter) string {
	var b [16]byte
	rand.Read(b[:])
	state := base64.URLEncoding.EncodeToString(b[:])
	cookie := http.Cookie{
		Name:     "oauthstate",
		Value:    state,
		Expires:  time.Now().Add(20 * time.Minute),
		HttpOnly: true,
		Secure:   false,
		Path:     "/",
	}
	http.SetCookie(w, &cookie)
	return state
}

func SetSessionCookie(w http.ResponseWriter, email string) {
	cookie := http.Cookie{
		Name:     "session_token",
		Value:    base64.URLEncoding.EncodeToString([]byte(email)),
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   false, // Set to true if using HTTPS strictly
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, &cookie)
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AuthDisabled {
			logger.Debugf("[AUTH] AuthDisabled is true. Bypassing authentication for %s", r.URL.Path)
			ctx := context.WithValue(r.Context(), UserEmailKey, "jjsong@whatap.io")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil {
			logger.Warnf("[AUTH] Session cookie missing for path: %s", r.URL.Path)
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}

		decodedEmailBytes, err := base64.URLEncoding.DecodeString(cookie.Value)
		if err != nil {
			logger.Errorf("[AUTH] Error decoding session cookie for %s: %v (Value: %s)", r.URL.Path, err, cookie.Value)
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}
		email := string(decodedEmailBytes)
		logger.Debugf("[AUTH] Valid session for %s: %s", r.URL.Path, email)

		ctx := context.WithValue(r.Context(), UserEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
