package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var GmailOauthConfig *oauth2.Config

func SetupGmailOAuth(cfg *config.Config) {
	GmailOauthConfig = &oauth2.Config{
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
	return GmailOauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func ExchangeGmailCode(ctx context.Context, code string) (*oauth2.Token, error) {
	return GmailOauthConfig.Exchange(ctx, code)
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

	tokenSource := GmailOauthConfig.TokenSource(ctx, &token)

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

func ScanGmail(ctx context.Context, email string, language string, cfg *config.Config) {
	logger.Debugf("[SCAN-GMAIL] Starting Gmail scan for %s", email)

	svc, err := GetGmailService(ctx, email)
	if err != nil {
		logger.Debugf("[SCAN-GMAIL] Skipping %s (no token or service error): %v", email, err)
		return
	}

	since := getGmailScanTime(email)
	query := fmt.Sprintf("in:inbox after:%d", since.Unix())

	res, err := svc.Users.Messages.List("me").Q(query).MaxResults(50).Do()
	if err != nil || len(res.Messages) == 0 {
		if err != nil {
			logger.Debugf("[SCAN-GMAIL] Failed to list messages for %s: %v", email, err)
		}
		return
	}

	rawMsgs, classificationMap, toMap := parseNewEmails(svc, res.Messages, email, cfg)
	if len(rawMsgs) == 0 {
		return
	}

	analyzeAndSaveEmails(ctx, email, language, rawMsgs, classificationMap, toMap, cfg)
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

func parseNewEmails(svc *gmail.Service, messages []*gmail.Message, email string, cfg *config.Config) ([]types.RawMessage, map[string]string, map[string]string) {
	var rawMsgs []types.RawMessage
	classificationMap := make(map[string]string)
	toMap := make(map[string]string)

	var skips []string
	if cfg.GmailSkipSenders != "" {
		for _, s := range strings.Split(cfg.GmailSkipSenders, ",") {
			s = strings.TrimSpace(strings.ToLower(s))
			if s != "" {
				skips = append(skips, s)
			}
		}
	}

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
		fromLower := strings.ToLower(from)
		isSkip := false
		for _, s := range skips {
			if strings.Contains(fromLower, s) {
				logger.Debugf("[SCAN-GMAIL] Skipping noise email from: %s (matches skip rule: %s)", from, s)
				isSkip = true
				break
			}
		}
		if isSkip {
			continue
		}
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

		rawMsgs = append(rawMsgs, types.RawMessage{
			ID:        m.Id,
			Sender:    from,
			Text:      fmt.Sprintf("To: %s, Subject: %s\nContent: %s", to, subject, body),
			Timestamp: time.Unix(fullMsg.InternalDate/1000, 0),
		})
	}

	return rawMsgs, classificationMap, toMap
}

func analyzeAndSaveEmails(ctx context.Context, email, language string, rawMsgs []types.RawMessage, classificationMap map[string]string, toMap map[string]string, cfg *config.Config) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)

	for _, m := range rawMsgs {
		msgMap[m.ID] = m
		sb.WriteString(fmt.Sprintf("[TS:%s] From: %s, %s\n---\n", m.ID, m.Sender, m.Text))
	}

	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
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
				assignee = ExtractNameFromEmail(toHeader)
			}
		} else if cls == "기타 업무" {
			isMe := strings.EqualFold(assignee, fallbackAssignee) || strings.EqualFold(assignee, user.Name) || strings.EqualFold(assignee, email)
			for _, alias := range aliases {
				if alias != "" && strings.EqualFold(assignee, alias) {
					isMe = true
					break
				}
			}
			if isMe {
				assignee = ExtractNameFromEmail(toHeader)
			}
		}

		assignedAt := time.Now().Format(time.RFC3339)
		origText := ""
		if m, ok := msgMap[item.SourceTS]; ok {
			assignedAt = m.Timestamp.Format(time.RFC3339)
			origText = m.Text
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
			OriginalText: origText,
		})
	}
}

func ExtractNameFromEmail(header string) string {
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

	if payload.MimeType == "text/plain" && payload.Body != nil && payload.Body.Data != "" {
		return decodeBase64URL(payload.Body.Data)
	}

	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			return decodeBase64URL(part.Body.Data)
		}
	}

	for _, part := range payload.Parts {
		if strings.HasPrefix(part.MimeType, "text/") && part.Body != nil && part.Body.Data != "" {
			return decodeBase64URL(part.Body.Data)
		}
		if result := extractBody(part); result != "" {
			return result
		}
	}

	return ""
}

func decodeBase64URL(data string) string {
	decoded, err := ai.DecodeBase64URL(data)
	if err != nil {
		return ""
	}
	return decoded
}
