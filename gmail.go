package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"net/http"
	"net/mail"
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
	tokenJSON, err := store.GetGmailToken(email)
	if err != nil {
		return nil, fmt.Errorf("no gmail token for %s: %w", email, err)
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("failed to parse gmail token for %s: %w", email, err)
	}

	tokenSource := gmailOauthConfig.TokenSource(ctx, &token)

	// Refresh and persist updated token if needed
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh gmail token for %s: %w", email, err)
	}
	if newToken.AccessToken != token.AccessToken {
		newTokenJSON, _ := json.Marshal(newToken)
		_ = store.SaveGmailToken(email, string(newTokenJSON))
	}

	svc, err := gmail.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create gmail service: %w", err)
	}
	return svc, nil
}

func ScanGmail(ctx context.Context, email string, language string) {
	logger.Debugf("[SCAN-GMAIL] Starting Gmail scan for %s", email)

	svc, err := GetGmailService(ctx, email)
	if err != nil {
		logger.Debugf("[SCAN-GMAIL] Skipping %s (no token or service error): %v", email, err)
		return
	}

	since := getGmailScanTime(email)
	query := fmt.Sprintf("in:inbox after:%d", since.Unix())

	msgs, err := svc.Users.Messages.List("me").Q(query).MaxResults(50).Do()
	if err != nil || len(msgs.Messages) == 0 {
		if err != nil {
			logger.Debugf("[SCAN-GMAIL] Failed to list messages for %s: %v", email, err)
		}
		return
	}

	rawMsgs, classificationMap, toMap := parseNewEmails(svc, msgs.Messages, email)
	if len(rawMsgs) == 0 {
		return
	}

	analyzeAndSaveEmails(ctx, email, language, rawMsgs, classificationMap, toMap)
	store.UpdateLastScan(email, "gmail", "inbox", fmt.Sprintf("%d", time.Now().Unix()))
}

func getGmailScanTime(email string) time.Time {
	lastTS := store.GetLastScan(email, "gmail", "inbox")
	if lastTS != "" {
		sec, _ := strconv.ParseInt(lastTS, 10, 64)
		return time.Unix(sec, 0)
	}
	return time.Now().Add(-7 * 24 * time.Hour)
}

func parseNewEmails(svc *gmail.Service, messages []*gmail.Message, email string) ([]RawMessage, map[string]string, map[string]string) {
	var rawMsgs []RawMessage
	classificationMap := make(map[string]string)
	toMap := make(map[string]string)

	for _, m := range messages {
		fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
		if err != nil {
			continue
		}

		var subject, from, to, cc, bcc string
		for _, h := range fullMsg.Payload.Headers {
			switch h.Name {
			case "Subject":
				subject = h.Value
			case "From":
				from = h.Value
			case "To":
				to = h.Value
			case "Cc":
				cc = h.Value
			case "Bcc":
				bcc = h.Value
			}
		}

		// Filter rules for user
		emailLower := strings.ToLower(email)
		toLower := strings.ToLower(to)
		ccLower := strings.ToLower(cc)
		bccLower := strings.ToLower(bcc)

		isDirect := strings.Contains(toLower, emailLower)
		isCc := strings.Contains(ccLower, emailLower)
		isBcc := strings.Contains(bccLower, emailLower)

		if !isDirect && !isCc && !isBcc {
			continue
		}

		classification := "기타 업무"
		if isDirect {
			classification = "내 업무"
		}

		classificationMap[m.Id] = classification
		toMap[m.Id] = to
		body := extractBody(fullMsg.Payload)

		rawMsgs = append(rawMsgs, RawMessage{
			ID:        m.Id,
			Sender:    from,
			Text:      fmt.Sprintf("To: %s, Subject: %s\nContent: %s", to, subject, body),
			Timestamp: time.Unix(fullMsg.InternalDate/1000, 0), // 이메일 실제 수신 시간
		})
	}

	return rawMsgs, classificationMap, toMap
}

func analyzeAndSaveEmails(ctx context.Context, email, language string, rawMsgs []RawMessage, classificationMap map[string]string, toMap map[string]string) {
	var sb strings.Builder
	msgMap := make(map[string]RawMessage)

	for _, m := range rawMsgs {
		msgMap[m.ID] = m
		sb.WriteString(fmt.Sprintf("[TS:%s] From: %s, %s\n---\n", m.ID, m.Sender, m.Text))
	}

	gc, err := NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Failed to init Gemini client: %v", err)
		return
	}

	items, err := gc.Analyze(ctx, email, sb.String(), language, "gmail")
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Gemini Analyze Error for %s: %v", email, err)
		return
	}

	user, _ := store.GetOrCreateUser(email, "", "")
	aliases, _ := store.GetUserAliases(user.ID)
	fallbackAssignee := user.Name
	if fallbackAssignee == "" {
		fallbackAssignee = email
	}

	for i, item := range items {
		link := fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", item.SourceTS)

		assignee := item.Assignee
		cls := classificationMap[item.SourceTS]
		toHeader := toMap[item.SourceTS]

		if assignee == "" || assignee == "me" || assignee == "나" || assignee == "담당자" {
			if cls == "내 업무" {
				assignee = fallbackAssignee
			} else {
				assignee = extractNameFromEmail(toHeader)
			}
		} else if cls == "기타 업무" {
			// 내가 CC/BCC로 수신한 경우, AI가 나를 담당자로 오탐하더라도 실제 To 수신자로 담당자 강제 교체
			isMe := strings.EqualFold(assignee, fallbackAssignee) || strings.EqualFold(assignee, user.Name) || strings.EqualFold(assignee, email)
			for _, alias := range aliases {
				if alias != "" && strings.EqualFold(assignee, alias) {
					isMe = true
					break
				}
			}
			if isMe {
				assignee = extractNameFromEmail(toHeader)
			}
		}

		assignedAt := time.Now().Format(time.RFC3339)
		if m, ok := msgMap[item.SourceTS]; ok {
			assignedAt = m.Timestamp.Format(time.RFC3339) // DB 저장 시 실제 이메일 수신 시간 기록
		}

		uniqueSourceTS := fmt.Sprintf("gmail-%s-%d", item.SourceTS, i)
		store.SaveMessage(store.ConsolidatedMessage{
			UserEmail:    email,
			Source:       "gmail",
			Room:         "Gmail",
			Task:         item.Task,
			Requester:    item.Requester,
			Assignee:     assignee,
			AssignedAt:   assignedAt,
			Link:         link,
			SourceTS:     uniqueSourceTS,
			OriginalText: item.OriginalText,
		})
	}
}

// extractNameFromEmail은 이메일 포맷("Name <email@domain.com>")에서 사람이 읽기 좋은 이름만 깔끔하게 추출합니다.
func extractNameFromEmail(header string) string {
	if header == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(header)
	if err == nil && len(addrs) > 0 {
		if addrs[0].Name != "" {
			return addrs[0].Name
		}
		return addrs[0].Address
	}

	firstRecip := strings.Split(header, ",")[0]
	if idx := strings.Index(firstRecip, "<"); idx != -1 {
		name := strings.TrimSpace(firstRecip[:idx])
		name = strings.Trim(name, "\"")
		if name != "" {
			return name
		}
		endIdx := strings.Index(firstRecip, ">")
		if endIdx > idx {
			return strings.TrimSpace(firstRecip[idx+1 : endIdx])
		}
	}
	return strings.TrimSpace(firstRecip)
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
		logger.Debugf("[GMAIL-CALLBACK] Token exchange failed for %s: %v", email, err)
		http.Error(w, "Token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	tokenJSON, err := json.Marshal(token)
	if err != nil {
		http.Error(w, "Failed to marshal token", http.StatusInternalServerError)
		return
	}

	if err := store.SaveGmailToken(email, string(tokenJSON)); err != nil {
		logger.Debugf("[GMAIL-CALLBACK] Failed to save token for %s: %v", email, err)
		http.Error(w, "Failed to save token", http.StatusInternalServerError)
		return
	}

	logger.Infof("[GMAIL-CALLBACK] Gmail connected for %s", email)
	http.Redirect(w, r, "/?gmail=connected", http.StatusTemporaryRedirect)
}

func handleGmailStatus(w http.ResponseWriter, r *http.Request) {
	email := GetUserEmail(r)
	connected := store.HasGmailToken(email)
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
