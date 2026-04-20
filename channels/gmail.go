package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"mime"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"

	"github.com/recapco/emailreplyparser"
)


const (
	CategorySent   = "발신 메일" //Why: Identifies emails sent by the user to determine if they constitute a commitment or a task update.
	CategoryMine   = "내 업무"  //Why: Marks emails where the user is the primary recipient as personal tasks.
	CategoryOthers = "기타 업무" //Why: Classifies CC'd or group emails as lower-priority informational items.
)

var genericMeAssignees = map[string]bool{
	"me": true, "나": true, "담당자": true, "__current_user__": true, //Why: Maps common self-referential terms across languages to the current user for consistent task assignment.
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
	tokenJSON, err := store.GetGmailToken(ctx, email)
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
		_ = store.SaveGmailToken(ctx, email, string(newTokenJSON))
	}

	svc, err := gmail.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("failed to create gmail service: %w", err)
	}
	return svc, nil
}

func ScanGmail(ctx context.Context, email string, language string, cfg *config.Config, onThreadActivity func(store.ConsolidatedMessage) bool) []int {
	svc, err := GetGmailService(ctx, email)
	if err != nil {
		logger.Debugf("[SCAN-GMAIL] Skipping %s: %v", email, err)
		return nil
	}

	since := getGmailScanTime(email)
	query := fmt.Sprintf("(in:inbox OR from:me) after:%d", since.Unix())
	allMsgs := fetchRecentEmails(svc, email, query)
	if len(allMsgs) == 0 {
		return nil
	}

	rawMsgs, clsMap, toMap, maxTS := parseNewEmails(ctx, svc, email, allMsgs, cfg)
	var newIDs []int
	if len(rawMsgs) > 0 {
		newIDs = analyzeAndSaveEmails(ctx, email, language, rawMsgs, clsMap, toMap, cfg, onThreadActivity)
	}

	if maxTS > 0 {
		store.UpdateLastScan(email, "gmail", "inbox", fmt.Sprintf("%d", maxTS))
	}
	return newIDs
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

func parseNewEmails(ctx context.Context, svc *gmail.Service, email string, messages []*gmail.Message, cfg *config.Config) ([]types.RawMessage, map[string]string, map[string]string, int64) {
	var rawMsgs []types.RawMessage
	classificationMap := make(map[string]string)
	toMap := make(map[string]string)
	var maxTS int64

	skips := getGmailSkips(cfg)

	for _, m := range messages {
		rawMsg, cls, to, ts, err := processSingleEmail(ctx, svc, email, m, skips)
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
func processSingleEmail(ctx context.Context, svc *gmail.Service, email string, m *gmail.Message, skips []string) (*types.RawMessage, string, string, int64, error) {
	fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
	if err != nil {
		return nil, "", "", 0, err
	}

	ts := fullMsg.InternalDate / 1000 // ms to s

	subject, fromHeader, toHeader, ccHeader, bccHeader, deliveredTo := extractHeaders(fullMsg.Payload.Headers)
	if isMarketingHeader(fullMsg.Payload.Headers) {
		logger.Debugf("[SCAN-GMAIL] Ignoring marketing email from: %s", fromHeader)
		store.IncrementFilteredCount(email)
		return nil, "", "", ts, nil
	}
	if isSkipSender(fromHeader, skips) {
		return nil, "", "", ts, nil
	}

	isFromMe, isDirect, isCc, isBcc, isDelTo := checkRecipientStatus(email, fromHeader, toHeader, ccHeader, bccHeader, deliveredTo)
	if !isFromMe && !isDirect && !isCc && !isBcc && !isDelTo {
		return nil, "", "", ts, nil
	}

	// Why: Automatically registers all participants (sender and recipients) in the contacts database to improve future identity resolution.
	senderEmail := upsertAddresses(ctx, email, fromHeader, "gmail")
	upsertAddresses(ctx, email, toHeader, "gmail")
	upsertAddresses(ctx, email, ccHeader, "gmail")

	classification := classifyGmail(isFromMe, isDirect)
	body := extractBody(fullMsg.Payload)
	cleanBody := cleanEmailBody(body)
	if cleanBody == "" {
		return nil, "", "", ts, nil
	}
	isImportant := false
	for _, lbl := range fullMsg.LabelIds {
		if lbl == "IMPORTANT" || lbl == "STARRED" {
			isImportant = true
			break
		}
	}

	attachmentNames := extractGmailAttachmentNames(fullMsg.Payload)

	rawMsg := &types.RawMessage{
		ID:              m.Id,
		Sender:          senderEmail, // Use canonical email as sender ID
		Text:            fmt.Sprintf("T: %s\nC: %s\nS: %s\nB:\n%s", toHeader, ccHeader, subject, cleanBody),
		Timestamp:       time.Unix(ts, 0),
		ThreadID:        fullMsg.ThreadId,
		IsImportant:     isImportant,
		HasAttachment:   len(attachmentNames) > 0,
		AttachmentNames: attachmentNames,
	}
	return rawMsg, classification, toHeader, ts, nil
}

// upsertAddresses parses a comma-separated list of email addresses and registers each one in the contacts store.
// It returns the email address of the first parsed contact for use as a primary identifier.
func upsertAddresses(ctx context.Context, tenantEmail, header, source string) string {
	if header == "" {
		return ""
	}

	//Why: Parses standard RFC 5322 format for multiple addresses and ensures display names are correctly decoded from MIME encoding.
	contacts, err := mail.ParseAddressList(header)
	if err != nil {
		logger.Debugf("[GMAIL] Failed to parse address list: %v", err)
		return types.ExtractNameFromEmail(header)
	}

	dec := new(mime.WordDecoder)
	firstEmail := ""

	for _, addr := range contacts {
		email := strings.ToLower(strings.TrimSpace(addr.Address))
		if email == "" {
			continue
		}
		if firstEmail == "" {
			firstEmail = email
		}

		name := addr.Name
		if decoded, err := dec.DecodeHeader(name); err == nil {
			name = decoded
		}
		_ = store.AutoUpsertContact(ctx, tenantEmail, email, name, source)
	}

	if firstEmail != "" {
		return firstEmail
	}
	return types.ExtractNameFromEmail(header)
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

// isMarketingHeader identifies promotional emails using standard headers like List-Unsubscribe and Precedence.
func isMarketingHeader(headers []*gmail.MessagePartHeader) bool {
	for _, h := range headers {
		if h.Name == "List-Unsubscribe" {
			return true
		}
		if h.Name == "Precedence" {
			val := strings.ToLower(h.Value)
			if val == "bulk" || val == "list" || val == "junk" {
				return true
			}
		}
	}
	return false
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

func analyzeAndSaveEmails(ctx context.Context, email, language string, rawMsgs []types.RawMessage, classificationMap map[string]string, toMap map[string]string, cfg *config.Config, onThreadActivity func(store.ConsolidatedMessage) bool) []int {
	gc, err := ai.NewGeminiClient(ctx, cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Failed to init Gemini client: %v", err)
		return nil
	}

	user, _ := store.GetOrCreateUser(ctx, email, "", "")
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	filterSvc := ai.NewGeminiLiteFilter(gc)

	var totalNewIDs []int
	batchSize := 10
	for i := 0; i < len(rawMsgs); i += batchSize {
		end := i + batchSize
		if end > len(rawMsgs) {
			end = len(rawMsgs)
		}
		ids := processBatch(ctx, gc, filterSvc, email, language, rawMsgs[i:end], classificationMap, toMap, user, aliases, onThreadActivity)
		totalNewIDs = append(totalNewIDs, ids...)
	}
	return totalNewIDs
}

// processBatch handles the analysis and persistence of a single batch of emails.
func processBatch(ctx context.Context, gc *ai.GeminiClient, filterSvc *ai.GeminiLiteFilter, email, language string, batchMsgs []types.RawMessage, classificationMap, toMap map[string]string, user *store.User, aliases []string, onThreadActivity func(store.ConsolidatedMessage) bool) []int {
	filteredMsgs := filterGmailBatch(ctx, email, batchMsgs, filterSvc, classificationMap, onThreadActivity)
	if len(filteredMsgs) == 0 {
		return nil
	}

	payload, msgMap := buildGmailBatchPayload(email, filteredMsgs, classificationMap)
	items, err := executeGmailAnalysisWithRetry(ctx, gc, email, payload, language, "Inbox")
	if err != nil {
		logger.Errorf("[SCAN-GMAIL] Batch Analyze Error for %s: %v", email, err)
		return nil
	}

	msgs := processGeminiItems(email, user, aliases, items, classificationMap, toMap, msgMap)
	var newIDs []int
	for i, item := range items {
		if i < len(msgs) {
			id, _ := store.HandleTaskState(ctx, store.GetDB(), email, item, msgs[i])
			if id > 0 {
				newIDs = append(newIDs, id)
			}
		}
	}
	return newIDs
}

// filterGmailBatch checks each message for processed status or AI-detected noise.
func filterGmailBatch(ctx context.Context, email string, batch []types.RawMessage, filterSvc *ai.GeminiLiteFilter, classificationMap map[string]string, onThreadActivity func(store.ConsolidatedMessage) bool) []types.RawMessage {
	var result []types.RawMessage
	for _, m := range batch {
		if processed, err := store.IsProcessed(ctx, store.GetDB(), email, m.ID); err == nil && processed {
			continue
		}
		// Why: [Early Return] Checks thread activity first. If handled as a completion/update, skips standard extraction.
		if onThreadActivity != nil && (classificationMap[m.ID] == CategorySent || classificationMap[m.ID] == CategoryMine || classificationMap[m.ID] == CategoryOthers) {
			if handled := onThreadActivity(store.ConsolidatedMessage{
				UserEmail: email, Source: "gmail", ThreadID: m.ThreadID,
				OriginalText: m.Text, SourceTS: m.ID,
			}); handled {
				_ = store.MarkAsProcessed(ctx, store.GetDB(), email, m.ID)
				continue
			}
		}
		if isNoise, err := filterSvc.IsNoise(ctx, email, m.Text); err == nil && isNoise {
			continue
		}
		result = append(result, m)
	}
	return result
}

// Why: Separates the payload construction and side-effects (onThreadActivity callback) from the main AI analysis loop.
func buildGmailBatchPayload(email string, batchMsgs []types.RawMessage, classificationMap map[string]string) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range batchMsgs {
		msgMap[m.ID] = m
		metaStr := buildGmailMetadataString(m)
		sb.WriteString(fmt.Sprintf("[ID:%s]%s F: %s\n%s\n---\n", m.ID, metaStr, m.Sender, m.Text))
	}
	return sb.String(), msgMap
}

func buildGmailMetadataString(m types.RawMessage) string {
	var tags []string
	if m.IsImportant {
		tags = append(tags, "Important")
	}
	if m.HasAttachment {
		tags = append(tags, "Has-Attachments")
	}

	var sb strings.Builder
	if len(tags) > 0 {
		sb.WriteString(fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", ")))
	}
	if len(m.AttachmentNames) > 0 {
		sb.WriteString(fmt.Sprintf(" [Files: %s]", strings.Join(m.AttachmentNames, ", ")))
	}
	return sb.String()
}

func extractGmailAttachmentNames(payload *gmail.MessagePart) []string {
	var names []string
	if payload == nil {
		return names
	}
	if payload.Filename != "" {
		names = append(names, payload.Filename)
	}
	for _, part := range payload.Parts {
		names = append(names, extractGmailAttachmentNames(part)...)
	}
	return names
}

// Why: Encapsulates the retry mechanism for Gemini API calls to keep the control flow clean.
func executeGmailAnalysisWithRetry(ctx context.Context, gc *ai.GeminiClient, email, payload, language, room string) ([]store.TodoItem, error) {
	var items []store.TodoItem
	var analyzeErr error
	// Why: [Metadata Enrichment] Creates a unified input for Gmail batches, using the payload as content.
	enriched := types.EnrichedMessage{
		RawContent:    payload,
		SourceChannel: "gmail",
		SenderID:      0,
		SenderName:    "Gmail System", // Generic for batch processing
		Timestamp:     time.Now(),
	}

	for attempt := 1; attempt <= 2; attempt++ {
		items, analyzeErr = gc.Analyze(ctx, email, enriched, language, "gmail", room)
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
	for i, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			logger.Warnf("[GMAIL-SCAN] Mismatch SourceTS: %s", item.SourceTS)
			continue
		}
		gmailCls := classificationMap[item.SourceTS]
		params := services.TaskBuildParams{
			UserEmail:           email,
			User:                *user,
			Aliases:             aliases,
			Item:                item,
			SenderRaw:           m.Sender,
			ToHeader:            toMap[item.SourceTS],
			Source:              "gmail",
			Room:                "Gmail",
			Link:                fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", item.SourceTS),
			SourceTS:            fmt.Sprintf("gmail-%s-%d", item.SourceTS, i),
			OriginalText:        m.Text,
			ThreadID:            m.ThreadID,
			SourceChannels:      []string{"gmail"},
			GmailClassification: gmailCls,
		}
		msgsToSave = append(msgsToSave, services.BuildTask(params))
	}
	return msgsToSave
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
	reWhitespace = regexp.MustCompile(`\s+`)
)

func stripHTML(raw string) string {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		// Why: Provides a graceful fallback to a simple whitespace-normalized version if the HTML parser fails, ensuring some level of sanitization.
		return strings.TrimSpace(reWhitespace.ReplaceAllString(raw, " "))
	}

	var buf bytes.Buffer
	var f func(*html.Node)
	f = func(n *html.Node) {
		// Why: Explicitly excludes script and style nodes and their entire subtrees to prevent their configuration or logic content from leaking into the extracted text.
		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" {
				return
			}
			// Why: Prunes Gmail reply quotes and blockquotes at the DOM level. This is 100% accurate unlike regex which often fails with nested history.
			if n.Data == "blockquote" {
				buf.WriteString(" ")
				return
			}
			for _, attr := range n.Attr {
				if attr.Key == "class" && (strings.Contains(attr.Val, "gmail_quote") || strings.Contains(attr.Val, "gmail_attr")) {
					buf.WriteString(" ")
					return
				}
			}
		}
		// Why: Skips comment nodes to further reduce noise in the extracted content.
		if n.Type == html.CommentNode {
			return
		}

		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}

		// Why: Injects a space after block-level elements to prevent words from being merged together.
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "br", "tr", "td", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				buf.WriteString(" ")
			}
		}
	}
	f(doc)

	// Why: Normalizes the final output by collapsing multi-line breaks and excessive spaces into a single space, and unescapes common HTML entities to ensure the text is human-readable.
	s := buf.String()
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(reWhitespace.ReplaceAllString(s, " "))
}

func decodeBase64URL(data string) string {
	decoded, err := ai.DecodeBase64URL(data)
	if err != nil {
		return ""
	}
	return decoded
}

// Why: cleanEmailBody strips signatures and quotes using emailreplyparser and ensures the body remains within AI token limits.
func cleanEmailBody(body string) string {
	if body == "" {
		return ""
	}

	// Use verified library to strip quoted text (latest reply only)
	email, err := emailreplyparser.Read(body)
	if err != nil {
		return truncateText(strings.TrimSpace(body), 3000)
	}

	var visibleFragments []string
	for _, f := range email.Fragments {
		// Library considers signatures "visible" but we want them hidden/removed
		if !f.Hidden && !f.Signature {
			visibleFragments = append(visibleFragments, f.String())
		}
	}

	result := strings.Join(visibleFragments, "\n")

	// Fallback: if library returns empty but original was not empty, use truncated original
	if strings.TrimSpace(result) == "" && strings.TrimSpace(body) != "" {
		result = body
	}

	return truncateText(strings.TrimSpace(result), 3000)
}

// truncateText caps the string to maxLen characters/runes safely.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "\n...[TRUNCATED]"
	}
	return s
}
