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

const (
	CategorySent   = "발신 메일"
	CategoryMine   = "내 업무"
	CategoryOthers = "기타 업무"
)

var genericMeAssignees = map[string]bool{
	"me": true, "나": true, "담당자": true,
}

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
	// (in:inbox OR from:me) to get both received and sent emails for thread context
	query := fmt.Sprintf("(in:inbox OR from:me) after:%d", since.Unix())

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

	skips := getGmailSkips(cfg)

	for _, m := range messages {
		fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
		if err != nil {
			continue
		}

		subject, from, to, cc, bcc := extractHeaders(fullMsg.Payload.Headers)
		if isSkipSender(from, skips) {
			continue
		}

		isFromMe, isDirect, isCc, isBcc := checkRecipientStatus(email, from, to, cc, bcc)
		if !isFromMe && !isDirect && !isCc && !isBcc {
			continue
		}

		classification := classifyGmail(isFromMe, isDirect)
		classificationMap[m.Id] = classification
		toMap[m.Id] = to

		body := extractBody(fullMsg.Payload)
		cleanBody := cleanEmailBody(body)
		if cleanBody == "" {
			continue
		}

		rawMsgs = append(rawMsgs, types.RawMessage{
			ID:        m.Id,
			Sender:    from,
			Text:      fmt.Sprintf("To: %s\nSubject: %s\nContent:\n%s", to, subject, cleanBody),
			Timestamp: time.Unix(fullMsg.InternalDate/1000, 0),
		})
	}

	return rawMsgs, classificationMap, toMap
}

func getGmailSkips(cfg *config.Config) []string {
	var skips []string
	if cfg.GmailSkipSenders == "" {
		return skips
	}
	for _, s := range strings.Split(cfg.GmailSkipSenders, ",") {
		s = strings.TrimSpace(strings.ToLower(s))
		if s != "" {
			skips = append(skips, s)
		}
	}
	return skips
}

func extractHeaders(headers []*gmail.MessagePartHeader) (subject, from, to, cc, bcc string) {
	for _, h := range headers {
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
	return
}

func isSkipSender(from string, skips []string) bool {
	fromLower := strings.ToLower(from)
	if strings.Contains(fromLower, "no-reply") || strings.Contains(fromLower, "noreply") || strings.Contains(fromLower, "do-not-reply") || strings.Contains(fromLower, "mailer-daemon") {
		return true
	}
	for _, s := range skips {
		if strings.Contains(fromLower, s) {
			logger.Debugf("[SCAN-GMAIL] Skipping noise email from: %s (matches skip rule: %s)", from, s)
			return true
		}
	}
	return false
}

func checkRecipientStatus(email, from, to, cc, bcc string) (isFromMe, isDirect, isCc, isBcc bool) {
	emailLower := strings.ToLower(email)
	isFromMe = strings.Contains(strings.ToLower(from), emailLower)
	isDirect = strings.Contains(strings.ToLower(to), emailLower)
	isCc = strings.Contains(strings.ToLower(cc), emailLower)
	isBcc = strings.Contains(strings.ToLower(bcc), emailLower)
	return
}

func classifyGmail(isFromMe, isDirect bool) string {
	if isFromMe {
		return CategorySent
	} else if isDirect {
		return CategoryMine
	}
	return CategoryOthers
}

// isAssigneeMe는 AI가 추출한 담당자(Assignee)가 현재 사용자 본인인지 판별합니다.
func isAssigneeMe(assignee, email, userName, fallback string, aliases []string) bool {
	lower := strings.ToLower(assignee)
	if lower == "" || genericMeAssignees[lower] {
		return true
	} else if strings.EqualFold(assignee, fallback) || strings.EqualFold(assignee, userName) || strings.EqualFold(assignee, email) {
		return true
	}
	for _, alias := range aliases {
		if alias != "" && strings.EqualFold(assignee, alias) {
			return true
		}
	}
	return false
}

func analyzeAndSaveEmails(ctx context.Context, email, language string, rawMsgs []types.RawMessage, classificationMap map[string]string, toMap map[string]string, cfg *config.Config) {
	if len(rawMsgs) == 0 {
		return
	}

	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Failed to init Gemini client: %v", err)
		return
	}

	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range rawMsgs {
		msgMap[m.ID] = m
		sb.WriteString(fmt.Sprintf("[TS:%s] From: %s, %s\n---\n", m.ID, m.Sender, m.Text))
	}

	items, err := gc.Analyze(ctx, email, sb.String(), language, "gmail")
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Gemini Analyze Error for %s: %v", email, err)
		return
	}

	user, _ := store.GetOrCreateUser(email, "", "")
	aliases, _ := store.GetUserAliases(user.ID)
	msgsToSave := processGeminiItems(email, user, aliases, items, classificationMap, toMap, msgMap)

	if len(msgsToSave) > 0 {
		store.SaveMessages(msgsToSave)
	}
}

func processGeminiItems(email string, user *store.User, aliases []string, items []store.TodoItem, classificationMap, toMap map[string]string, msgMap map[string]types.RawMessage) []store.ConsolidatedMessage {
	var msgsToSave []store.ConsolidatedMessage
	fallbackAssignee := getPreferredName(user)

	for i, item := range items {
		isMe := isAssigneeMe(item.Assignee, email, user.Name, fallbackAssignee, aliases)
		cls := classificationMap[item.SourceTS]
		toHeader := toMap[item.SourceTS]

		assignee, category := resolveGmailCategoryAndAssignee(item, isMe, cls, toHeader, fallbackAssignee)

		assignedAt := time.Now()
		origText := ""
		if m, ok := msgMap[item.SourceTS]; ok {
			assignedAt = m.Timestamp
			origText = m.Text
		}

		msgsToSave = append(msgsToSave, store.ConsolidatedMessage{
			UserEmail:    email,
			Source:       "gmail",
			Room:         "Gmail",
			Task:         item.Task,
			Requester:    item.Requester,
			Assignee:     assignee,
			AssignedAt:   assignedAt,
			Link:         fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", item.SourceTS),
			SourceTS:     fmt.Sprintf("gmail-%s-%d", item.SourceTS, i),
			OriginalText: origText,
			Deadline:     item.Deadline,
			Category:     category,
		})
	}
	return msgsToSave
}

func resolveGmailCategoryAndAssignee(item store.TodoItem, isMe bool, cls, toHeader, fallback string) (string, string) {
	assignee := item.Assignee
	category := item.Category

	if cls == CategorySent {
		if isMe {
			return fallback, "promise"
		}
		return assignee, "waiting"
	}

	if isMe && cls == CategoryMine {
		assignee = fallback
	} else if (isMe && cls == CategoryOthers) || assignee == "" {
		assignee = ExtractNameFromEmail(toHeader)
	}
	return assignee, category
}

func getPreferredName(user *store.User) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Email
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

// cleanEmailBody는 이메일 본문에서 이전 스레드(인용구)와 서명을 제거하여 순수한 새 메시지만 추출합니다.
func cleanEmailBody(body string) string {
	lines := strings.Split(body, "\n")
	var cleaned []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 1. 영어/한국어 일반적인 답장 인용구 시작 패턴 차단
		if (strings.HasPrefix(trimmed, "On ") && strings.HasSuffix(trimmed, "wrote:")) ||
			strings.HasSuffix(trimmed, "작성:") ||
			strings.HasPrefix(trimmed, "-----Original Message-----") ||
			strings.HasPrefix(trimmed, "----- 원본 메시지 -----") ||
			strings.HasPrefix(trimmed, "________________________________") {
			break // 인용구 이하는 모두 버림
		}

		// 2. '>' 로 시작하는 인용 라인 건너뛰기
		if strings.HasPrefix(trimmed, ">") {
			continue
		}

		cleaned = append(cleaned, line)
	}

	// 3. 서명(Signature) 제거 패턴 (--) 감지
	result := strings.Join(cleaned, "\n")
	if idx := strings.Index(result, "\n-- \n"); idx != -1 {
		result = result[:idx]
	}

	return strings.TrimSpace(result)
}
