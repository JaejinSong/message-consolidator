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
	CategorySent   = "발신 메일" //Why: Identifies emails sent by the user to determine if they constitute a commitment or a task update.
	CategoryMine   = "내 업무"   //Why: Marks emails where the user is the primary recipient as personal tasks.
	CategoryOthers = "기타 업무" //Why: Classifies CC'd or group emails as lower-priority informational items.
)

var genericMeAssignees = map[string]bool{
	"me": true, "나": true, "담당자": true, //Why: Maps common self-referential terms across languages to the current user for consistent task assignment.
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

	//Why: Automatically refreshes the OAuth2 token if it has expired and persists the new token to the database to ensure uninterrupted Gmail access.
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
	//Why: Searches both the inbox and sent items to capture the full conversation context, allowing the AI to correctly identify task status and assignees.
	query := fmt.Sprintf("(in:inbox OR from:me) after:%d", since.Unix())
	logger.Debugf("[SCAN-GMAIL] Query: %s", query)

	allMsgs := fetchRecentEmails(svc, email, query)

	if len(allMsgs) == 0 {
		return
	}

	logger.Infof("[SCAN-GMAIL] Found %d new messages for %s", len(allMsgs), email)
	rawMsgs, classificationMap, toMap, maxTS := parseNewEmails(svc, email, allMsgs, cfg)
	if len(rawMsgs) == 0 {
		//Why: Updates the 'last scan' timestamp even if no tasks were found, ensuring subsequent scans don't re-process the same volume of irrelevant emails.
		if maxTS > 0 {
			store.UpdateLastScan(email, "gmail", "inbox", fmt.Sprintf("%d", maxTS))
		}
		return
	}

	//Why: Triggers the AI-powered analysis of extracted emails and saves valid tasks to the database.
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

// Why: Isolates the pagination and fetching logic to keep the main scanning workflow concise.
func fetchRecentEmails(svc *gmail.Service, email, query string) []*gmail.Message {
	var allMsgs []*gmail.Message
	pageToken := ""
	for {
		res, err := svc.Users.Messages.List("me").Q(query).PageToken(pageToken).MaxResults(100).Do()
		if err != nil {
			logger.Errorf("[SCAN-GMAIL] List Error for %s: %v", email, err)
			return allMsgs
		}
		allMsgs = append(allMsgs, res.Messages...)
		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
		//Why: Implements a fetch limit of 1000 emails to prevent potential infinite loops or excessive memory consumption during initial or deep scans.
		if len(allMsgs) >= 1000 {
			break
		}
	}
	return allMsgs
}

func parseNewEmails(svc *gmail.Service, email string, messages []*gmail.Message, cfg *config.Config) ([]types.RawMessage, map[string]string, map[string]string, int64) {
	var rawMsgs []types.RawMessage
	classificationMap := make(map[string]string)
	toMap := make(map[string]string)
	var maxTS int64

	skips := getGmailSkips(cfg)

	for _, m := range messages {
		rawMsg, cls, to, ts, err := processSingleEmail(svc, email, m, skips)
		if err != nil {
			logger.Errorf("[SCAN-GMAIL] Get Error for %s: %v", m.Id, err)
			continue
		}
		if ts > maxTS {
			maxTS = ts
		}
		if rawMsg != nil {
			rawMsgs = append(rawMsgs, *rawMsg)
			classificationMap[m.Id] = cls
			toMap[m.Id] = to
		}
	}

	return rawMsgs, classificationMap, toMap, maxTS
}

// Why: Extracts the processing of a single email to reduce cognitive load and simplify the main parsing loop.
func processSingleEmail(svc *gmail.Service, email string, m *gmail.Message, skips []string) (*types.RawMessage, string, string, int64, error) {
	fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
	if err != nil {
		return nil, "", "", 0, err
	}

	ts := fullMsg.InternalDate / 1000 // ms to s

	subject, from, to, cc, bcc, deliveredTo := extractHeaders(fullMsg.Payload.Headers)
	if isSkipSender(from, skips) {
		return nil, "", "", ts, nil
	}

	isFromMe, isDirect, isCc, isBcc, isDelTo := checkRecipientStatus(email, from, to, cc, bcc, deliveredTo)
	if !isFromMe && !isDirect && !isCc && !isBcc && !isDelTo {
		return nil, "", "", ts, nil
	}

	classification := classifyGmail(isFromMe, isDirect)
	body := extractBody(fullMsg.Payload)
	cleanBody := cleanEmailBody(body)
	if cleanBody == "" {
		return nil, "", "", ts, nil
	}

	rawMsg := &types.RawMessage{
		ID:        m.Id,
		Sender:    from,
		Text:      fmt.Sprintf("T: %s\nC: %s\nS: %s\nB:\n%s", to, cc, subject, cleanBody),
		Timestamp: time.Unix(ts, 0),
		ThreadID:  fullMsg.ThreadId,
	}
	return rawMsg, classification, to, ts, nil
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

// Why: Determines the relationship between the user and the email headers (To, Cc, Bcc, Delivered-To) to decide how the email should be classified and prioritized.
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

	//Why: Allows partial name matches to account for cases where Slack real names include supplementary information like parentheses or suffixes.
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

	//Why: Groups emails into small batches of 10 for AI analysis to optimize prompt length and stay within model context and token generation limits.
	batchSize := 10
	for i := 0; i < len(rawMsgs); i += batchSize {
		end := i + batchSize
		if end > len(rawMsgs) {
			end = len(rawMsgs)
		}
		batchMsgs := rawMsgs[i:end]

		payload, msgMap := buildGmailBatchPayload(email, batchMsgs, classificationMap, onSent)

		items, analyzeErr := executeGmailAnalysisWithRetry(ctx, gc, email, payload, language)
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

// Why: Separates the payload construction and side-effects (onSent callback) from the main AI analysis loop.
func buildGmailBatchPayload(email string, batchMsgs []types.RawMessage, classificationMap map[string]string, onSent func(store.ConsolidatedMessage)) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range batchMsgs {
		msgMap[m.ID] = m
		sb.WriteString(fmt.Sprintf("[ID:%s] F: %s\n%s\n---\n", m.ID, m.Sender, m.Text))

		//Why: Analyzes sent emails to determine if a previously identified task can be marked as completed based on the user's reply.
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
	return sb.String(), msgMap
}

// Why: Encapsulates the retry mechanism for Gemini API calls to keep the control flow clean.
func executeGmailAnalysisWithRetry(ctx context.Context, gc *ai.GeminiClient, email, payload, language string) ([]store.TodoItem, error) {
	var items []store.TodoItem
	var analyzeErr error
	//Why: Implements a simple retry mechanism for AI analysis calls to handle transient network issues or unexpected JSON formatting errors from the model.
	for attempt := 1; attempt <= 2; attempt++ {
		items, analyzeErr = gc.Analyze(ctx, email, payload, language, "gmail")
		if analyzeErr == nil {
			return items, nil
		}
		logger.Warnf("[SCAN-GMAIL] Gemini Analyze retry %d for %s: %v", attempt, email, analyzeErr)
		time.Sleep(1 * time.Second)
	}
	return nil, analyzeErr
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
		//Why: CategoryOthers (CC or group mail) OR not identifying as me. Force group/recipient name as assignee to avoid "me" categorization in UI.
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
			//Why: Decodes MIME-encoded headers to ensure non-ASCII characters (like Korean names) are correctly displayed in the UI.
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

	//Why: Falls back to stripping HTML tags from the body if a plain-text version of the email is unavailable.
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
	reScript      = regexp.MustCompile(`(?i)<script.*?>.*?</script>`)
	reStyle       = regexp.MustCompile(`(?i)<style.*?>.*?</style>`)
	reComment     = regexp.MustCompile(`<!--.*?-->`)
	reTags        = regexp.MustCompile(`<.*?>`)
	reWhitespace  = regexp.MustCompile(`\s+`)
	reQuoteStart  = regexp.MustCompile(`(?i)^(on\s+.*\swrote:\s*$|.*님이\s*작성:\s*$|-----\s*original message\s*-----|-----\s*원본 메시지\s*-----|_{10,}|>)`)
	reSignature   = regexp.MustCompile(`(?m)^--\s*$`) //Why: Identifies standard MIME signature delimiters to truncate noise at the end of emails.
	reEmailHeader = regexp.MustCompile(`(?i)^(from|to|cc|bcc|subject|date|sent):\s`) //Why: Detects embedded headers in reply chains to accurately split original content from quoted history.
)

func stripHTML(html string) string {
	//Why: Strips non-content HTML elements like scripts and styles first to reduce token usage and prevent AI confusion during analysis.
	s := reScript.ReplaceAllString(html, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reComment.ReplaceAllString(s, "")

	//Why: Removes all remaining HTML tags to clean up the content for better AI comprehension.
	s = reTags.ReplaceAllString(s, " ")

	//Why: Unescapes HTML entities and normalizes whitespace to provide a clean, human-readable string to the AI.
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")

	//Why: Compresses excessive whitespace to minimize total token count and improve pattern matching performance.
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

//Why: cleanEmailBody strips signatures and keeps only the top portion of quoted replies to maintain context while keeping prompt sizes manageable.
func cleanEmailBody(body string) string {
	lines := strings.Split(body, "\n")
	var cleaned []string
	isQuoted := false
	quotedLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		//Why: Identifies the start of reply threads to isolate the newest content while preserving minimal context for the AI.
		if !isQuoted && reQuoteStart.MatchString(trimmed) {
			isQuoted = true
			//Why: Retains the quotation header while excluding it from the line limit to give the AI context about who the user is replying to.
			cleaned = append(cleaned, line)
			continue
		}

		if isQuoted {
			//Why: Filters out repetitive header information within quote blocks to focus analysis on the actual message content.
			if reEmailHeader.MatchString(trimmed) || trimmed == "" && quotedLines == 0 {
				cleaned = append(cleaned, line)
				continue
			}
			quotedLines++
			//Why: Limits depth of quoted replies to 5 lines to maintain focus on the current task while providing enough context for thread association.
			if quotedLines > 5 {
				break
			}
		}

		cleaned = append(cleaned, line)
	}

	result := strings.Join(cleaned, "\n")

	//Why: Removes email signatures to prevent them from being mistaken for task requests or assignee names.
	if loc := reSignature.FindStringIndex(result); loc != nil {
		result = result[:loc[0]]
	}

	finalResult := strings.TrimSpace(result)

	//Why: Caps the total email length at 3000 characters to prevent the AI model from truncating its JSON response because the input was too large.
	const maxLen = 3000
	if len(finalResult) > maxLen {
		//Why: Truncates content using a rune slice to avoid creating invalid UTF-8 sequences at the truncation point, which could cause processing errors.
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
