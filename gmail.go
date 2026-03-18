package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

	// Fetch messages depuis le last scan
	lastTS := GetLastScan(email, "gmail", "inbox")
	var since time.Time
	if lastTS != "" {
		sec, _ := strconv.ParseInt(lastTS, 10, 64)
		since = time.Unix(sec, 0)
	} else {
		since = time.Now().Add(-7 * 24 * time.Hour)
	}
	
	query := fmt.Sprintf("in:inbox after:%d", since.Unix())

	msgs, err := svc.Users.Messages.List("me").Q(query).MaxResults(50).Do()
	if err != nil {
		log.Printf("[SCAN-GMAIL] Failed to list messages for %s: %v", email, err)
		return false
	}

	if len(msgs.Messages) == 0 {
		return false
	}
	
	nowTS := fmt.Sprintf("%d", time.Now().Unix())
	hasNew := false
	
	// Collect all email contents to send to Gemini at once for efficiency
	var sb strings.Builder
	contentMap := make(map[string]string) // MsgID -> FullContent for modal
	classificationMap := make(map[string]string) // MsgID -> classification
	
	for _, m := range msgs.Messages {
		fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
		if err != nil {
			continue
		}
		
		subject := ""
		from := ""
		to := ""
		cc := ""
		bcc := ""
		body := ""
		date := ""
		
		for _, h := range fullMsg.Payload.Headers {
			if h.Name == "Subject" {
				subject = h.Value
			}
			if h.Name == "From" {
				from = h.Value
			}
			if h.Name == "To" {
				to = h.Value
			}
			if h.Name == "Cc" {
				cc = h.Value
			}
			if h.Name == "Bcc" {
				bcc = h.Value
			}
			if h.Name == "Date" {
				date = h.Value
			}
		}

		// Filter rules for jjsong@whatap.io
		isDirect := strings.Contains(strings.ToLower(to), "jjsong@whatap.io")
		isCc := strings.Contains(strings.ToLower(cc), "jjsong@whatap.io")
		isBcc := strings.Contains(strings.ToLower(bcc), "jjsong@whatap.io")

		// If not explicitly in To, Cc or Bcc, exclude it (covers group mail exclusion)
		if !isDirect && !isCc && !isBcc {
			continue
		}

		classification := "기타 업무"
		if isDirect {
			classification = "내 업무"
		}

		
		body = extractBody(fullMsg.Payload)

		fullEmailContent := fmt.Sprintf("Subject: %s\nFrom: %s\nDate: %s\n\n%s", subject, from, date, body)
		contentMap[m.Id] = fullEmailContent
		// Store classification for this message
		classificationMap[m.Id] = classification
		sb.WriteString(fmt.Sprintf("[TS:%s] From: %s, To: %s, Subject: %s\nContent: %s\n---\n", m.Id, from, to, subject, body))
	}
	
	if sb.Len() > 0 {
		gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey)
		if err != nil {
			return false
		}
		
		items, err := gc.Analyze(ctx, sb.String(), language)
		if err != nil {
			log.Printf("[SCAN-GMAIL] Gemini Analyze Error: %v", err)
			return false
		}
		
		for i, item := range items {
			link := fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", item.SourceTS)
			
			// Use classification from our map
			assignee := classificationMap[item.SourceTS]
			if assignee == "" {
				assignee = item.Assignee
			}

			// Store individual tasks
			uniqueSourceTS := fmt.Sprintf("gmail-%s-%d", item.SourceTS, i)
			
			originalText := item.OriginalText

			saved, _ := SaveMessage(ConsolidatedMessage{
				UserEmail:    email,
				Source:       "gmail",
				Room:         "Gmail",
				Task:         item.Task,
				Requester:    item.Requester,
				Assignee:     assignee,
				AssignedAt:   time.Now().Format(time.RFC3339),
				Link:         link,
				SourceTS:     uniqueSourceTS,
				OriginalText: originalText,
			})
			if saved {
				hasNew = true
			}
		}
	}
	
	// 새 메시지가 실제로 저장된 경우에만 scanTS 갱신 (DB Sleep 최적화)
	if hasNew {
		UpdateLastScan(email, "gmail", "inbox", nowTS)
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
