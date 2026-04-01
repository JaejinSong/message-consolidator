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
	appBaseURL        string
)

type contextKey string

const UserEmailKey contextKey = "userEmail"

func GetUserEmail(r *http.Request) string {
	if AuthDisabled {
		return "jjsong@whatap.io" //Why: Provides a static fallback user for local development environments where OAuth is unavailable or disabled.
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
		RedirectURL:  fmt.Sprintf("%s/api/auth/callback", cfg.AppBaseURL),
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

	//Why: Synchronizes the Google user metadata with the local database to ensure user records stay current across logins.
	user, err := store.GetOrCreateUser(userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		logger.Errorf("Failed to sync user to DB: %v", err)
	} else {
		//Why: Employs a function callback pattern to perform cross-service Slack ID resolution without creating a circular package dependency.
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
	isSecure := strings.HasPrefix(appBaseURL, "https://")
	
	//Why: Explicitly invalidates the server-side session token by clearing the corresponding cookie on the client.
	cookie := http.Cookie{
		Name:     "session_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   isSecure,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	}
	if isSecure {
		cookie.SameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &cookie)

	//Why: Removes the non-HttpOnly hint cookie so the frontend can immediately react to the logged-out state.
	hintCookie := http.Cookie{
		Name:     "session_active",
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: false,
		Secure:   isSecure,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	}
	if isSecure {
		hintCookie.SameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &hintCookie)

	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

//Why: Generates a cryptographically secure random string for use as the OAuth2 'state' parameter to prevent CSRF attacks.
func generateStateCookie(w http.ResponseWriter) string {
	var b [16]byte
	rand.Read(b[:])
	state := base64.RawURLEncoding.EncodeToString(b[:])
	isSecure := strings.HasPrefix(appBaseURL, "https://")
	sameSite := http.SameSiteLaxMode
	if isSecure {
		sameSite = http.SameSiteNoneMode
	}

	cookie := http.Cookie{
		Name:     "oauthstate",
		Value:    state,
		Expires:  time.Now().Add(20 * time.Minute),
		HttpOnly: true,
		Secure:   isSecure,
		Path:     "/",
		SameSite: sameSite,
	}
	http.SetCookie(w, &cookie)
	return state
}

func SetSessionCookie(w http.ResponseWriter, email string) {
	isSecure := strings.HasPrefix(appBaseURL, "https://")
	sameSite := http.SameSiteLaxMode
	if isSecure {
		sameSite = http.SameSiteNoneMode
	}
	
	//Why: Establishes a server-side session using an HttpOnly cookie to prevent XSS-based token theft.
	cookie := http.Cookie{
		Name:     "session_token",
		Value:    base64.RawURLEncoding.EncodeToString([]byte(email)),
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   isSecure,
		Path:     "/",
		SameSite: sameSite,
	}
	http.SetCookie(w, &cookie)

	//Why: Provides a public "session active" hint that the frontend can read without exposing the actual sensitive session token.
	hintCookie := http.Cookie{
		Name:     "session_active",
		Value:    "true",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: false, // Accessible by JS
		Secure:   isSecure,
		Path:     "/",
		SameSite: sameSite,
	}
	http.SetCookie(w, &hintCookie)
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "unauthorized", "code": 401})
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
			json.NewEncoder(w).Encode(map[string]interface{}{"error": "unauthorized", "code": 401})
			return
		}
		email := string(decodedEmailBytes)
		logger.Debugf("[AUTH] Valid session for %s: %s", r.URL.Path, email)

		ctx := context.WithValue(r.Context(), UserEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
