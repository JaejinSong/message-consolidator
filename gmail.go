package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var gmailOauthConfig *oauth2.Config

func SetupGmailOAuth() {
	gmailOauthConfig = &oauth2.Config{
		RedirectURL:  fmt.Sprintf("%s/auth/gmail/callback", cfg.AppBaseURL),
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		Scopes: []string{
			"https://www.googleapis.com/auth/gmail.readonly",
		},
		Endpoint: google.Endpoint,
	}
}

func GetGmailAuthURL(state string) string {
	return gmailOauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func ExchangeGmailCode(ctx context.Context, code string) (*oauth2.Token, error) {
	return gmailOauthConfig.Exchange(ctx, code)
}

func GetGmailService(ctx context.Context, email string) (*gmail.Service, error) {
	tokenJSON, err := GetGmailToken(email)
	if err != nil {
		return nil, fmt.Errorf("no gmail token for %s: %w", email, err)
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("failed to parse gmail token: %w", err)
	}

	tokenSource := gmailOauthConfig.TokenSource(ctx, &token)

	// Refresh and persist updated token if needed
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh gmail token for %s: %w", email, err)
	}
	if newToken.AccessToken != token.AccessToken {
		newTokenJSON, _ := json.Marshal(newToken)
		_ = SaveGmailToken(email, string(newTokenJSON))
	}

	svc, err := gmail.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create gmail service: %w", err)
	}
	return svc, nil
}

func ScanGmail(ctx context.Context, email string, language string) bool {
	log.Printf("[SCAN-GMAIL] Starting Gmail scan for %s", email)

	svc, err := GetGmailService(ctx, email)
	if err != nil {
		log.Printf("[SCAN-GMAIL] Skipping %s (no token or service error): %v", email, err)
		return false
	}

	// Fetch messages from the last 7 days
	since := time.Now().Add(-7 * 24 * time.Hour)
	query := fmt.Sprintf("in:inbox after:%d", since.Unix())

	msgs, err := svc.Users.Messages.List("me").Q(query).MaxResults(50).Do()
	if err != nil {
		log.Printf("[SCAN-GMAIL] Failed to list messages for %s: %v", email, err)
		return false
	}

	if len(msgs.Messages) == 0 {
		log.Printf("[SCAN-GMAIL] No new emails for %s", email)
		return false
	}

	log.Printf("[SCAN-GMAIL] Found %d emails for %s", len(msgs.Messages), email)
	
	var sb strings.Builder
	msgMap := make(map[string]time.Time)     // message-id -> received time
	contentMap := make(map[string]string)   // message-id -> full content (Subject + Body)
	
	for _, msgRef := range msgs.Messages {
		msg, err := svc.Users.Messages.Get("me", msgRef.Id).Format("full").Do()
		if err != nil {
			log.Printf("[SCAN-GMAIL] Failed to get message %s: %v", msgRef.Id, err)
			continue
		}
		
		subject := getHeader(msg.Payload.Headers, "Subject")
		from := getHeader(msg.Payload.Headers, "From")
		body := extractBody(msg.Payload)
		if body == "" {
			continue
		}
		
		receivedAt := time.Unix(msg.InternalDate/1000, 0)
		msgMap[msgRef.Id] = receivedAt
		
		msgContent := fmt.Sprintf("Subject: %s\nFrom: %s\n\n%s", subject, from, body)
		contentMap[msgRef.Id] = msgContent
		
		sb.WriteString(fmt.Sprintf("[TS:%s] [%s] %s\n\n",
			msgRef.Id,
			receivedAt.Format("15:04"),
			msgContent,
		))
	}
	
	if sb.Len() == 0 {
		log.Printf("[SCAN-GMAIL] No readable content for %s", email)
		return false
	}
	
	gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey)
	if err != nil {
		log.Printf("[SCAN-GMAIL] Failed to create Gemini client: %v", err)
		return false
	}
	
	items, err := gc.Analyze(ctx, sb.String(), language)
	if err != nil {
		log.Printf("[SCAN-GMAIL] Gemini analyze error for %s: %v", email, err)
		return false
	}
	
	log.Printf("[SCAN-GMAIL] Gemini extracted %d tasks for %s", len(items), email)
	
	hasNew := false
	for i, item := range items {
		assignedAt := time.Now().Format(time.RFC3339)
		if ts, ok := msgMap[item.SourceTS]; ok {
			assignedAt = ts.Format(time.RFC3339)
		}
		
		link := ""
		if item.SourceTS != "" {
			link = fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", item.SourceTS)
		}
		
		// Use the full email content as OriginalText
		originalText := contentMap[item.SourceTS]
		if originalText == "" {
			originalText = item.OriginalText
		}
		
		// Multi-task support: Ensure each task from the same email has a unique SourceTS
		uniqueSourceTS := fmt.Sprintf("gmail-%s-%d", item.SourceTS, i)
		
		saved, _ := SaveMessage(ConsolidatedMessage{
			UserEmail:    email,
			Source:       "gmail",
			Room:         "Inbox",
			Task:         item.Task,
			Requester:    item.Requester,
			Assignee:     item.Assignee,
			AssignedAt:   assignedAt,
			Link:         link,
			SourceTS:     uniqueSourceTS,
			OriginalText: originalText,
		})
		if saved {
			hasNew = true
		}
	}
	
	return hasNew
}

func getHeader(headers []*gmail.MessagePartHeader, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func extractBody(payload *gmail.MessagePart) string {
	if payload == nil {
		return ""
	}

	// Prefer text/plain
	if payload.MimeType == "text/plain" && payload.Body != nil && payload.Body.Data != "" {
		return decodeBase64URL(payload.Body.Data)
	}

	// Recurse into parts
	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			return decodeBase64URL(part.Body.Data)
		}
	}

	// Fallback: try any text/*
	for _, part := range payload.Parts {
		if strings.HasPrefix(part.MimeType, "text/") && part.Body != nil && part.Body.Data != "" {
			return decodeBase64URL(part.Body.Data)
		}
		// Recurse for multipart
		if result := extractBody(part); result != "" {
			return result
		}
	}

	return ""
}

func handleGmailConnect(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	state := "gmail:" + email
	url := GetGmailAuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func handleGmailCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	if !strings.HasPrefix(state, "gmail:") {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	email := strings.TrimPrefix(state, "gmail:")

	token, err := ExchangeGmailCode(ctx, code)
	if err != nil {
		log.Printf("[GMAIL-CALLBACK] Token exchange failed for %s: %v", email, err)
		http.Error(w, "Token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		http.Error(w, "Failed to marshal token", http.StatusInternalServerError)
		return
	}

	if err := SaveGmailToken(email, string(tokenJSON)); err != nil {
		log.Printf("[GMAIL-CALLBACK] Failed to save token for %s: %v", email, err)
		http.Error(w, "Failed to save token", http.StatusInternalServerError)
		return
	}

	log.Printf("[GMAIL-CALLBACK] Gmail connected for %s", email)
	http.Redirect(w, r, "/?gmail=connected", http.StatusTemporaryRedirect)
}

func handleGmailStatus(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	connected := HasGmailToken(email)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"connected": connected})
}

func decodeBase64URL(data string) string {
	decoded, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		// Try standard encoding as fallback
		decoded, err = base64.StdEncoding.DecodeString(data)
		if err != nil {
			return ""
		}
	}
	return string(decoded)
}
