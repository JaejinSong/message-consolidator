package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	googleOauthConfig *oauth2.Config
	AuthDisabled      bool
)

// SetupOAuth initializes the Google OAuth2 config
type contextKey string

const userEmailKey contextKey = "userEmail"

func GetUserEmail(r *http.Request) string {
	if AuthDisabled {
		return "jjsong@whatap.io" // Default user when auth is disabled
	}
	email, ok := r.Context().Value(userEmailKey).(string)
	if !ok || email == "" {
		return "jjsong@whatap.io"
	}
	return email
}

func SetupOAuth() {
	googleOauthConfig = &oauth2.Config{
		RedirectURL:  fmt.Sprintf("%s/auth/callback", cfg.AppBaseURL),
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		Scopes:       []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint:     google.Endpoint,
	}
}

func handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	state := generateStateCookie(w)
	url := googleOauthConfig.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	oauthState, _ := r.Cookie("oauthstate")

	if r.FormValue("state") != oauthState.Value {
		errorf("Invalid oauth google state")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	token, err := googleOauthConfig.Exchange(context.Background(), r.FormValue("code"))
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
	user, err := GetOrCreateUser(userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		errorf("Failed to sync user to DB: %v", err)
	} else {
		// Automatically attempt to find the user's Slack ID and Aliases
		sc := NewSlackClient(os.Getenv("SLACK_TOKEN"))
		slackUser, err := sc.LookupUserByEmail(user.Email)
		if err == nil && slackUser != nil {
			UpdateUserSlackID(user.Email, slackUser.ID)
			AddUserAlias(user.ID, slackUser.RealName)
			if slackUser.Profile.DisplayName != "" {
				AddUserAlias(user.ID, slackUser.Profile.DisplayName)
			}
			infof("Auto-discovered Slack ID %s and aliases for %s", slackUser.ID, user.Email)
		}
	}

	setSessionCookie(w, userInfo.Email)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
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

func setSessionCookie(w http.ResponseWriter, email string) {
	// For production, this should be signed/encrypted with AuthSecret
	// Since we are adding it for the first time, we'll keep it simple
	// but secure with HttpOnly and Secure flags.
	cookie := http.Cookie{
		Name:     "session_token",
		Value:    base64.URLEncoding.EncodeToString([]byte(email)),
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		Secure:   false,
		Path:     "/",
	}
	http.SetCookie(w, &cookie)
}

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AuthDisabled {
			ctx := context.WithValue(r.Context(), userEmailKey, "jjsong@whatap.io")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		cookie, err := r.Cookie("session_token")
		if err != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}

		decodedEmailBytes, err := base64.URLEncoding.DecodeString(cookie.Value)
		if err != nil {
			debugf("Error decoding session cookie: %v", err)
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}
		email := string(decodedEmailBytes)

		ctx := context.WithValue(r.Context(), userEmailKey, email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
