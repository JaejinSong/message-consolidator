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
	"mime"
	"net/mail"
	"regexp"
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

func ScanGmail(ctx context.Context, email string, language string, cfg *config.Config, onSent func(store.ConsolidatedMessage)) {
	logger.Debugf("[SCAN-GMAIL] Starting Gmail scan for %s", email)

	svc, err := GetGmailService(ctx, email)
	if err != nil {
		logger.Debugf("[SCAN-GMAIL] Skipping %s (no token or service error): %v", email, err)
		return
	}

	since := getGmailScanTime(email)
	// (in:inbox OR from:me) to get both received and sent emails for thread context
	query := fmt.Sprintf("(in:inbox OR from:me) after:%d", since.Unix())
	logger.Debugf("[SCAN-GMAIL] Query: %s", query)

	var allMsgs []*gmail.Message
	pageToken := ""
	for {
		res, err := svc.Users.Messages.List("me").Q(query).PageToken(pageToken).MaxResults(100).Do()
		if err != nil {
			logger.Errorf("[SCAN-GMAIL] List Error for %s: %v", email, err)
			return
		}
		allMsgs = append(allMsgs, res.Messages...)
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
		// Safety limit: max 10 pages (~1000 emails) to prevent infinite loops
		if len(allMsgs) >= 1000 {
			break
		}
	}

	if len(allMsgs) == 0 {
		return
	}

	logger.Infof("[SCAN-GMAIL] Found %d new messages for %s", len(allMsgs), email)
	rawMsgs, classificationMap, toMap, maxTS := parseNewEmails(svc, email, allMsgs, cfg)
	if len(rawMsgs) == 0 {
		// Update maxTS for fresh scans even if no actionable messages were found
		if maxTS > 0 {
			store.UpdateLastScan(email, "gmail", "inbox", fmt.Sprintf("%d", maxTS))
		}
		return
	}

	analyzeAndSaveEmails(ctx, email, language, rawMsgs, classificationMap, toMap, cfg, onSent)

	if maxTS > 0 {
		store.UpdateLastScan(email, "gmail", "inbox", fmt.Sprintf("%d", maxTS))
	}
}

func getGmailScanTime(email string) time.Time {
	lastTS := store.GetLastScan(email, "gmail", "inbox")
	if lastTS != "" {
		sec, _ := strconv.ParseInt(lastTS, 10, 64)
		return time.Unix(sec, 0)
	}
	return time.Now().Add(-7 * 24 * time.Hour)
}

func parseNewEmails(svc *gmail.Service, email string, messages []*gmail.Message, cfg *config.Config) ([]types.RawMessage, map[string]string, map[string]string, int64) {
	var rawMsgs []types.RawMessage
	classificationMap := make(map[string]string)
	toMap := make(map[string]string)
	var maxTS int64

	skips := getGmailSkips(cfg)

	for _, m := range messages {
		fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
		if err != nil {
			logger.Errorf("[SCAN-GMAIL] Get Error for %s: %v", m.Id, err)
			continue
		}

		ts := fullMsg.InternalDate / 1000 // ms to s
		if ts > maxTS {
			maxTS = ts
		}

		subject, from, to, cc, bcc, deliveredTo := extractHeaders(fullMsg.Payload.Headers)
		if isSkipSender(from, skips) {
			continue
		}

		isFromMe, isDirect, isCc, isBcc, isDelTo := checkRecipientStatus(email, from, to, cc, bcc, deliveredTo)
		if !isFromMe && !isDirect && !isCc && !isBcc && !isDelTo {
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
			Text:      fmt.Sprintf("T: %s\nC: %s\nS: %s\nB:\n%s", to, cc, subject, cleanBody),
			Timestamp: time.Unix(fullMsg.InternalDate/1000, 0),
			ThreadID:  fullMsg.ThreadId,
		})
	}

	return rawMsgs, classificationMap, toMap, maxTS
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

func extractHeaders(headers []*gmail.MessagePartHeader) (subject, from, to, cc, bcc, deliveredTo string) {
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
		case "Delivered-To":
			deliveredTo = h.Value
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

func checkRecipientStatus(email, from, to, cc, bcc, deliveredTo string) (isFromMe, isDirect, isCc, isBcc, isDelTo bool) {
	emailLower := strings.ToLower(email)
	isFromMe = strings.Contains(strings.ToLower(from), emailLower)
	isDirect = strings.Contains(strings.ToLower(to), emailLower)
	isCc = strings.Contains(strings.ToLower(cc), emailLower)
	isBcc = strings.Contains(strings.ToLower(bcc), emailLower)
	isDelTo = strings.Contains(strings.ToLower(deliveredTo), emailLower)
	return
}

func classifyGmail(isFromMe, isTo bool) string {
	if isFromMe {
		return CategorySent
	} else if isTo {
		return CategoryMine
	}
	return CategoryOthers
}

// isAssigneeMe checks if the AI-extracted assignee refers to the current user.
func isAssigneeMe(assignee, email, userName, fallback string, aliases []string) bool {
	if assignee == "" || strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown") {
		return false
	}
	lowerAsg := strings.ToLower(assignee)
	if genericMeAssignees[lowerAsg] {
		return true
	}

	lowerEmail := strings.ToLower(email)
	lowerName := strings.ToLower(userName)
	lowerFallback := strings.ToLower(fallback)

	// Direct matches
	if lowerAsg == lowerEmail || lowerAsg == lowerName || lowerAsg == lowerFallback {
		return true
	}

	// Partial name match (e.g., "Jaejin Song" matches "Jaejin Song (JJ)")
	if len(lowerAsg) > 3 && (strings.Contains(lowerName, lowerAsg) || strings.Contains(lowerAsg, lowerName)) {
		return true
	}

	for _, alias := range aliases {
		if alias != "" && strings.EqualFold(assignee, alias) {
			return true
		}
	}
	return false
}

func analyzeAndSaveEmails(ctx context.Context, email, language string, rawMsgs []types.RawMessage, classificationMap map[string]string, toMap map[string]string, cfg *config.Config, onSent func(store.ConsolidatedMessage)) {
	if len(rawMsgs) == 0 {
		return
	}

	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Failed to init Gemini client: %v", err)
		return
	}

	user, _ := store.GetOrCreateUser(email, "", "")
	aliases, _ := store.GetUserAliases(user.ID)

	// Process emails in batches of 10 to avoid AI token limits
	batchSize := 10
	for i := 0; i < len(rawMsgs); i += batchSize {
		end := i + batchSize
		if end > len(rawMsgs) {
			end = len(rawMsgs)
		}
		batchMsgs := rawMsgs[i:end]

		var sb strings.Builder
		msgMap := make(map[string]types.RawMessage)
		for _, m := range batchMsgs {
			msgMap[m.ID] = m
			sb.WriteString(fmt.Sprintf("[ID:%s] F: %s\n%s\n---\n", m.ID, m.Sender, m.Text))

			// Check for completion if it's a sent mail
			if classificationMap[m.ID] == CategorySent && onSent != nil {
				onSent(store.ConsolidatedMessage{
					UserEmail:    email,
					Source:       "gmail",
					ThreadID:     m.ThreadID,
					OriginalText: m.Text,
					SourceTS:     m.ID,
				})
			}
		}

		var items []store.TodoItem
		var analyzeErr error
		// Retry up to 2 times for transient AI JSON parsing errors
		for attempt := 1; attempt <= 2; attempt++ {
			items, analyzeErr = gc.Analyze(ctx, email, sb.String(), language, "gmail")
			if analyzeErr == nil {
				break
			}
			logger.Warnf("[SCAN-GMAIL] Gemini Analyze retry %d for %s: %v", attempt, email, analyzeErr)
			time.Sleep(1 * time.Second)
		}

		if analyzeErr != nil {
			logger.Errorf("[SCAN-GMAIL] Gemini Analyze Error for %s (batch %d-%d): %v", email, i, end, analyzeErr)
			continue
		}

		msgsToSave := processGeminiItems(email, user, aliases, items, classificationMap, toMap, msgMap)
		if len(msgsToSave) > 0 {
			store.SaveMessages(msgsToSave)
		}
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
		m, ok := msgMap[item.SourceTS]
		if !ok {
			logger.Warnf("[GMAIL-SCAN] Mismatch SourceTS: %s. Skipping this task item.", item.SourceTS)
			continue
		}
		assignedAt = m.Timestamp
		origText = m.Text

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
			ThreadID:     msgMap[item.SourceTS].ThreadID,
		})
	}
	return msgsToSave
}

func resolveGmailCategoryAndAssignee(item store.TodoItem, isMe bool, cls, toHeader, fallback string) (string, string) {
	assignee := item.Assignee
	if strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown") {
		assignee = ""
	}
	category := item.Category

	if cls == CategorySent {
		if isMe {
			return fallback, "promise"
		}
		return assignee, "waiting"
	}

	if isMe && cls == CategoryMine {
		assignee = fallback
	} else {
		// CategoryOthers (CC or group mail) OR not identifying as me
		// Force group/recipient name as assignee to avoid "me" categorization in UI
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
			// Decode RFC 2047 encoded email headers (e.g., =?UTF-8?B?...?=)
			dec := new(mime.WordDecoder)
			if decoded, err := dec.DecodeHeader(name); err == nil {
				name = decoded
			}
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

	// If no plain text, try HTML and convert to text
	if payload.MimeType == "text/html" && payload.Body != nil && payload.Body.Data != "" {
		return stripHTML(decodeBase64URL(payload.Body.Data))
	}

	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			return decodeBase64URL(part.Body.Data)
		}
	}

	for _, part := range payload.Parts {
		if part.MimeType == "text/html" && part.Body != nil && part.Body.Data != "" {
			return stripHTML(decodeBase64URL(part.Body.Data))
		}
		if result := extractBody(part); result != "" {
			return result
		}
	}

	return ""
}

var (
	reScript     = regexp.MustCompile(`(?i)<script.*?>.*?</script>`)
	reStyle      = regexp.MustCompile(`(?i)<style.*?>.*?</style>`)
	reComment    = regexp.MustCompile(`<!--.*?-->`)
	reTags       = regexp.MustCompile(`<.*?>`)
	reWhitespace = regexp.MustCompile(`\s+`)
	reQuoteStart  = regexp.MustCompile(`(?i)^(on\s+.*\swrote:\s*$|.*님이\s*작성:\s*$|-----\s*original message\s*-----|-----\s*원본 메시지\s*-----|_{10,}|>)`)
	reSignature   = regexp.MustCompile(`(?m)^--\s*$`)
	reEmailHeader = regexp.MustCompile(`(?i)^(from|to|cc|bcc|subject|date|sent):\s`)
)

func stripHTML(html string) string {
	// 1. Remove script/style/comments first
	s := reScript.ReplaceAllString(html, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reComment.ReplaceAllString(s, "")

	// 2. Remove all other tags
	s = reTags.ReplaceAllString(s, " ")

	// 3. Basic cleanup: replace multiple spaces/newlines and unescape entities
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")

	// 4. Collapse multiple spaces and newlines for cleaner AI input
	s = reWhitespace.ReplaceAllString(s, " ")

	return strings.TrimSpace(s)
}

func decodeBase64URL(data string) string {
	decoded, err := ai.DecodeBase64URL(data)
	if err != nil {
		return ""
	}
	return decoded
}

// cleanEmailBody strips signatures and keeps only the top portion of quoted replies for context.
func cleanEmailBody(body string) string {
	lines := strings.Split(body, "\n")
	var cleaned []string
	isQuoted := false
	quotedLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 1. Detect quote blocks using Regex
		if !isQuoted && reQuoteStart.MatchString(trimmed) {
			isQuoted = true
			// Include the trigger line (e.g. "On ... wrote:") but don't count it
			// against the 5-line content limit.
			cleaned = append(cleaned, line)
			continue
		}

		if isQuoted {
			// Skip email header lines (From:, To:, Cc:, Subject:) without counting against limit
			if reEmailHeader.MatchString(trimmed) || trimmed == "" && quotedLines == 0 {
				cleaned = append(cleaned, line)
				continue
			}
			quotedLines++
			// Keep up to 5 lines of quoted content for context
			if quotedLines > 5 {
				break
			}
		}

		cleaned = append(cleaned, line)
	}

	result := strings.Join(cleaned, "\n")

	// 2. Detect and remove signatures using Regex
	if loc := reSignature.FindStringIndex(result); loc != nil {
		result = result[:loc[0]]
	}

	finalResult := strings.TrimSpace(result)

	// Safety fallback: truncate extremely long emails to prevent AI output JSON truncation
	const maxLen = 3000
	if len(finalResult) > maxLen {
		// Use rune slice to safely truncate UTF-8 strings without breaking multi-byte characters
		runes := []rune(finalResult)
		if len(runes) > maxLen/2 {
			limit := maxLen
			if len(runes) < limit {
				limit = len(runes)
			}
			finalResult = string(runes[:limit]) + "\n...[TRUNCATED]"
		}
	}

	return finalResult
}
