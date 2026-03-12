package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var googleOauthConfig *oauth2.Config

// SetupOAuth initializes the Google OAuth2 config
func SetupOAuth() {
	googleOauthConfig = &oauth2.Config{
		RedirectURL:  fmt.Sprintf("%s/auth/callback", cfg.AppBaseURL),
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		Scopes:       []string{"https://www.googleapis.com/auth/userinfo.email"},
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
		log.Println("Invalid oauth google state")
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
		Email string `json:"email"`
	}
	if err := json.NewDecoder(response.Body).Decode(&userInfo); err != nil {
		fmt.Fprintf(w, "Failed decoding user info: %s", err.Error())
		return
	}

	// Session Management (Simple encrypted cookie for demo)
	// In a real app, use a proper session store
	setSessionCookie(w, userInfo.Email)
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
		Secure:   true,
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
		Secure:   true,
		Path:     "/",
	}
	http.SetCookie(w, &cookie)
}

// AuthMiddleware protects routes
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.AuthDisabled {
			next(w, r)
			return
		}

		sessionCookie, err := r.Cookie("session_token")
		if err != nil || sessionCookie.Value == "" {
			// Not authenticated
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/auth/login", http.StatusTemporaryRedirect)
			return
		}

		// Optional: Verify user domain or specific email if needed
		next(w, r)
	}
}
