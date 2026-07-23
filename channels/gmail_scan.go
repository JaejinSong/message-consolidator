package channels

import (
	"context"
	"fmt"
	"message-consolidator/ai"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
)

func ScanGmail(ctx context.Context, email string, language string, cfg *config.Config, gc *ai.GeminiClient, filterSvc *ai.GeminiLiteFilter, onThreadActivity func(store.ConsolidatedMessage) bool) []store.MessageID {
	svc, err := GetGmailService(ctx, email)
	if err != nil {
		// Why: Warn, not Debug — production suppresses Debug, and a dead token
		// silently disabled Gmail scanning for 15 days (2026-07-08 incident).
		logger.Warnf("[GMAIL] scan skipped for %s: %v", email, err)
		return nil
	}

	since := getGmailScanTime(email)
	query := fmt.Sprintf("(in:inbox OR from:me) after:%d", since.Unix())
	allMsgs, fetchOK := fetchRecentEmails(svc, email, query)
	if len(allMsgs) == 0 {
		if fetchOK {
			markGmailScanSuccess(email)
		}
		return nil
	}

	rawMsgs, clsMap, toMap, maxTS, parseOK := parseNewEmails(ctx, svc, email, allMsgs, cfg)
	var newIDs []store.MessageID
	analyzeOK := true
	if len(rawMsgs) > 0 {
		newIDs, analyzeOK = analyzeAndSaveEmails(ctx, email, language, rawMsgs, clsMap, toMap, gc, filterSvc, onThreadActivity)
	}

	// Why: A failed Get/Analyze leaves messages unmarked; advancing the cursor past them
	// skips them forever (2026-07-23 backlog loss after a 45s scan timeout). Hold the
	// cursor so the next cycle retries — IsProcessed keeps already-handled IDs cheap.
	if !fetchOK || !parseOK || !analyzeOK {
		logger.Warnf("[GMAIL] cursor held for %s (fetchOK=%v parseOK=%v analyzeOK=%v); unprocessed messages retry next cycle", email, fetchOK, parseOK, analyzeOK)
		return newIDs
	}
	if maxTS > 0 {
		if err := store.UpdateLastScan(email, store.SourceGmail, "inbox", fmt.Sprintf("%d", maxTS)); err != nil {
			logger.Warnf("[GMAIL] UpdateLastScan failed for %s: %v", email, err)
		}
	}
	markGmailScanSuccess(email)
	return newIDs
}

// markGmailScanSuccess stamps the wall-clock time of the last clean scan pass.
// Why: /gmail/status checks token presence only; every silent failure mode (dead token,
// held cursor, fetch error) must surface as staleness in the UI instead of "connected".
func markGmailScanSuccess(email string) {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	if err := store.UpdateLastScan(email, store.SourceGmail, store.ScanTargetLastSuccess, ts); err != nil {
		logger.Warnf("[GMAIL] record last_success failed for %s: %v", email, err)
	}
}

func getGmailScanTime(email string) time.Time {
	lastTS := store.GetLastScan(email, store.SourceGmail, "inbox")
	if lastTS != "" {
		sec, _ := strconv.ParseInt(lastTS, 10, 64)
		return time.Unix(sec, 0)
	}
	return time.Now().Add(-7 * 24 * time.Hour)
}

// fetchRecentEmails lists message IDs matching query. ok=false signals a truncated
// list (API error mid-pagination) so callers can distinguish "no new mail" from
// "fetch failed" — an empty-but-failed fetch must not count as a successful scan.
func fetchRecentEmails(svc *gmail.Service, email, query string) ([]*gmail.Message, bool) {
	var allMsgs []*gmail.Message
	pageToken := ""
	for {
		res, err := svc.Users.Messages.List("me").Q(query).PageToken(pageToken).MaxResults(100).Do()
		if err != nil {
			logger.Errorf("[GMAIL] list error for %s: %v", email, err)
			return allMsgs, false
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
	return allMsgs, true
}

func parseNewEmails(ctx context.Context, svc *gmail.Service, email string, messages []*gmail.Message, cfg *config.Config) ([]types.RawMessage, map[string]string, map[string]string, int64, bool) {
	var rawMsgs []types.RawMessage
	classificationMap := make(map[string]string)
	toMap := make(map[string]string)
	var maxTS int64
	ok := true

	skips := getGmailSkips(cfg)
	internalDomains := cfg.CompanyDomains

	for _, m := range messages {
		// Why: Gmail's `after:since` is second-precision inclusive, so messages at the
		// boundary timestamp re-appear every cycle. Skip Gmail Get + header parse +
		// contact upsert for IDs we've already processed. maxTS is unaffected — those
		// messages were already counted in a prior scan and UpdateLastScan committed.
		if processed, _ := store.IsProcessed(ctx, store.GetDB(), email, m.Id); processed {
			continue
		}
		rawMsg, cls, to, ts, err := processSingleEmail(ctx, svc, email, m, skips, internalDomains)
		if err != nil {
			logger.Errorf("[GMAIL] get error for %s: %v", m.Id, err)
			// Why: This message is neither marked processed nor analyzed — the cursor must not advance past it.
			ok = false
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

	return rawMsgs, classificationMap, toMap, maxTS, ok
}

// Why: Extracts the processing of a single email to reduce cognitive load and simplify the main parsing loop.
func markFilteredEmail(ctx context.Context, email, msgID string) {
	store.IncrementFilteredCount(email)
	_ = store.MarkAsProcessed(ctx, store.GetDB(), email, msgID)
}

func markProcessedEmail(ctx context.Context, email, msgID string) {
	_ = store.MarkAsProcessed(ctx, store.GetDB(), email, msgID)
}

func processSingleEmail(ctx context.Context, svc *gmail.Service, email string, m *gmail.Message, skips []string, internalDomains []string) (*types.RawMessage, string, string, int64, error) {
	fullMsg, err := svc.Users.Messages.Get("me", m.Id).Format("full").Do()
	if err != nil {
		return nil, "", "", 0, err
	}

	ts := fullMsg.InternalDate / 1000 // ms to s

	subject, fromHeader, toHeader, ccHeader, bccHeader, deliveredTo := extractHeaders(fullMsg.Payload.Headers)
	// Why: Each terminal filter below marks the message ID processed so the next scan
	// cycle's parseNewEmails IsProcessed check skips Gmail Get + header parse +
	// contact upsert entirely. Without marking, Gmail's `after:` boundary inclusivity
	// re-fetches these messages every cycle and burns Google API quota for nothing.
	if isMarketingHeader(fullMsg.Payload.Headers, fromHeader, internalDomains) {
		logger.Debugf("[GMAIL] ignoring marketing email from: %s", fromHeader)
		markFilteredEmail(ctx, email, m.Id)
		return nil, "", "", ts, nil
	}
	if isSelfAddressedBulk(fromHeader, toHeader, email) {
		logger.Debugf("[GMAIL] ignoring self-addressed bulk from: %s", fromHeader)
		markFilteredEmail(ctx, email, m.Id)
		return nil, "", "", ts, nil
	}
	if isSystemOriginEmail(fullMsg.Payload.Headers, subject) {
		logger.Debugf("[GMAIL] ignoring system-origin email: %s", subject)
		markFilteredEmail(ctx, email, m.Id)
		return nil, "", "", ts, nil
	}
	if isSkipSender(fromHeader, skips) {
		markProcessedEmail(ctx, email, m.Id)
		return nil, "", "", ts, nil
	}

	isFromMe, isDirect, isCc, isBcc, isDelTo := checkRecipientStatus(email, fromHeader, toHeader, ccHeader, bccHeader, deliveredTo)
	if !isFromMe && !isDirect && !isCc && !isBcc && !isDelTo {
		if !hasLabel(fullMsg.LabelIds, "INBOX") {
			markProcessedEmail(ctx, email, m.Id)
			return nil, "", "", ts, nil
		}
		// Why: User not in any header but message is in inbox — delivered via mailing list
		// (e.g. indonesia@whatap.io group). Treat as implicit Cc so it routes to Reference tab.
		isCc = true
	}

	// Why: Automatically registers all participants (sender and recipients) in the contacts database to improve future identity resolution.
	senderEmail, senderName := upsertAddresses(ctx, email, fromHeader, store.SourceGmail)
	upsertAddresses(ctx, email, toHeader, store.SourceGmail)
	upsertAddresses(ctx, email, ccHeader, store.SourceGmail)

	classification := classifyGmail(isFromMe, isDirect)
	cleanBody := cleanEmailBody(extractBody(fullMsg.Payload))
	if cleanBody == "" {
		markProcessedEmail(ctx, email, m.Id)
		return nil, "", "", ts, nil
	}

	rawMsg := assembleGmailRawMessage(fullMsg, m.Id, ts, senderEmail, senderName, subject, toHeader, ccHeader, cleanBody, isFromMe, isDirect, isCc, isBcc, isDelTo)
	return rawMsg, classification, toHeader, ts, nil
}

// assembleGmailRawMessage builds the RawMessage from already-parsed fields. Extracted
// to keep processSingleEmail's cyclomatic complexity under the project ceiling (≤15)
// after the four MarkAsProcessed early-return branches were added — the label scan
// and CcOnly multi-conjunction live here so the parent function only carries
// filter/skip control flow.
func assembleGmailRawMessage(fullMsg *gmail.Message, msgID string, ts int64, senderEmail, senderName, subject, toHeader, ccHeader, cleanBody string, isFromMe, isDirect, isCc, isBcc, isDelTo bool) *types.RawMessage {
	attachmentNames := extractGmailAttachmentNames(fullMsg.Payload)
	return &types.RawMessage{
		ID:              msgID,
		Sender:          senderEmail,
		SenderName:      senderName,
		Text:            fmt.Sprintf("T: %s\nC: %s\nS: %s\nB:\n%s", toHeader, ccHeader, subject, cleanBody),
		Timestamp:       time.Unix(ts, 0),
		ThreadID:        fullMsg.ThreadId,
		IsImportant:     hasImportantLabel(fullMsg.LabelIds),
		HasAttachment:   len(attachmentNames) > 0,
		AttachmentNames: attachmentNames,
		IsFromMe:        isFromMe,
		IsCcOnly:        isCc && !isFromMe && !isDirect && !isBcc && !isDelTo,
	}
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

func bodyPrefix(text string, n int) string {
	idx := strings.Index(text, "B:\n")
	if idx < 0 {
		return ""
	}
	body := strings.ToLower(reWhitespace.ReplaceAllString(text[idx+3:], " "))
	if len(body) > n {
		return body[:n]
	}
	return body
}

func envelopeKey(m types.RawMessage) string {
	prefix := bodyPrefix(m.Text, 150)
	if prefix == "" {
		return ""
	}
	return m.Sender + "#" + m.Timestamp.UTC().Format("2006-01-02") + "#" + prefix
}

// Why: Gmail can deliver the same logical email as N message objects; collapse by sender+day+body-prefix before AI sees them.
func deduplicateEnvelopes(ctx context.Context, email string, rawMsgs []types.RawMessage) []types.RawMessage {
	seen := make(map[string]int)
	var result []types.RawMessage
	for _, m := range rawMsgs {
		key := envelopeKey(m)
		if key == "" {
			result = append(result, m)
			continue
		}
		if _, exists := seen[key]; !exists {
			seen[key] = len(result)
			result = append(result, m)
		} else {
			_ = store.MarkAsProcessed(ctx, store.GetDB(), email, m.ID)
		}
	}
	return result
}

func analyzeAndSaveEmails(ctx context.Context, email, language string, rawMsgs []types.RawMessage, classificationMap map[string]string, toMap map[string]string, gc *ai.GeminiClient, filterSvc *ai.GeminiLiteFilter, onThreadActivity func(store.ConsolidatedMessage) bool) ([]store.MessageID, bool) {
	if gc == nil || filterSvc == nil {
		logger.Errorf("[GMAIL] gc/filterSvc missing; scanner.Init may have failed")
		return nil, false
	}

	user, _ := store.GetOrCreateUser(ctx, email, "", "")
	aliases, _ := store.GetUserAliases(ctx, user.ID)
	rawMsgs = deduplicateEnvelopes(ctx, email, rawMsgs)

	var totalNewIDs []store.MessageID
	ok := true
	batchSize := 10
	for i := 0; i < len(rawMsgs); i += batchSize {
		end := i + batchSize
		if end > len(rawMsgs) {
			end = len(rawMsgs)
		}
		ids, batchOK := processBatch(ctx, gc, filterSvc, email, language, rawMsgs[i:end], classificationMap, toMap, user, aliases, onThreadActivity)
		if !batchOK {
			ok = false
		}
		totalNewIDs = append(totalNewIDs, ids...)
	}
	return totalNewIDs, ok
}

// processBatch handles the analysis and persistence of a single batch of emails.
func processBatch(ctx context.Context, gc *ai.GeminiClient, filterSvc *ai.GeminiLiteFilter, email, language string, batchMsgs []types.RawMessage, classificationMap, toMap map[string]string, user *store.User, aliases []string, onThreadActivity func(store.ConsolidatedMessage) bool) ([]store.MessageID, bool) {
	filteredMsgs := filterGmailBatch(ctx, email, batchMsgs, filterSvc, classificationMap, onThreadActivity)
	if len(filteredMsgs) == 0 {
		return nil, true
	}

	payload, msgMap := buildGmailBatchPayload(email, filteredMsgs, classificationMap)
	enriched := types.EnrichedMessage{
		RawContent:    payload,
		SourceChannel: store.SourceGmail,
		SenderID:      0,
		SenderName:    "Gmail System",
		Timestamp:     time.Now(),
	}
	items, err := gc.Analyze(ctx, email, enriched, language, store.SourceGmail, "Inbox")
	if err != nil {
		logger.Errorf("[GMAIL] batch analyze error for %s: %v", email, err)
		// Why: Batch members are unmarked at this point — signal the caller to hold the scan cursor.
		return nil, false
	}

	// Why: AI cost is sunk once Analyze returns, so mark every batch member processed
	// regardless of HandleTaskState outcome. Without this, `messages.source_ts` is
	// stored with a "gmail-" prefix while IsProcessed checks raw m.ID — mismatch
	// leaves processBatch-routed messages eligible for re-extraction every cycle,
	// burning prompt tokens with no new state to find.
	for _, m := range filteredMsgs {
		_ = store.MarkAsProcessed(ctx, store.GetDB(), email, m.ID)
	}

	msgByTS := processGeminiItems(ctx, email, user, aliases, items, classificationMap, toMap, msgMap)
	var newIDs []store.MessageID
	for _, item := range items {
		msg, ok := msgByTS[item.SourceTS]
		if !ok {
			continue
		}
		if id, _ := services.HandleTaskState(ctx, store.GetDB(), email, item, msg); id > 0 {
			newIDs = append(newIDs, id)
		}
	}
	return newIDs, true
}

// noiseFilter is the consumer-side contract for the AI noise gate.
// Why: defined here (channels) instead of accepting *ai.GeminiLiteFilter so unit tests
// can inject a fake without spinning up a real Gemini client.
type noiseFilter interface {
	IsNoise(ctx context.Context, email, source, text string) (bool, error)
}

// filterGmailBatch checks each message for AI-detected noise or thread completion status.
// Why: IsProcessed early-skip moved upstream to parseNewEmails so already-processed
// IDs never reach this batch. Remaining duties: LiteFilter noise gate + thread-completion
// routing. Noise gate runs FIRST: a thread reply that is actually marketing/newsletter must
// not slip into ProcessPotentialCompletion.fallbackToNewExtraction, which executes Analyze
// directly without LiteFilter and would persist the spam as a task. IsNoise=true marks the
// message processed so the next cycle's parseNewEmails short-circuits before paying
// LiteFilter again.
func filterGmailBatch(ctx context.Context, email string, batch []types.RawMessage, filterSvc noiseFilter, classificationMap map[string]string, onThreadActivity func(store.ConsolidatedMessage) bool) []types.RawMessage {
	var result []types.RawMessage
	for _, m := range batch {
		if isNoise, err := filterSvc.IsNoise(ctx, email, store.SourceGmail, m.Text); err == nil && isNoise {
			_ = store.MarkAsProcessed(ctx, store.GetDB(), email, m.ID)
			continue
		}
		if handleThreadActivity(ctx, email, m, classificationMap, onThreadActivity) {
			continue
		}
		result = append(result, m)
	}
	return result
}

// Why: Reply/sent threads enter the completion pipeline before standard extraction, so the early-return path lives here to keep filterGmailBatch flat.
func handleThreadActivity(ctx context.Context, email string, m types.RawMessage, classificationMap map[string]string, onThreadActivity func(store.ConsolidatedMessage) bool) bool {
	if onThreadActivity == nil {
		return false
	}
	cls := classificationMap[m.ID]
	if cls != CategorySent && cls != CategoryMine && cls != CategoryOthers {
		return false
	}
	requester := m.SenderName
	if requester == "" {
		requester = m.Sender
	}
	cm := store.ConsolidatedMessage{
		UserEmail: email, Source: store.SourceGmail, Room: "Gmail", ThreadID: m.ThreadID,
		OriginalText: m.Text, SourceTS: m.ID, Requester: requester,
		// Why: fallbackToNewExtraction persists a new task from this cm — a zero
		// AssignedAt lands as assigned_at=NULL, disabling aging/deadline/stalled.
		// CreatedAt feeds relative-deadline resolution in the fallback Analyze.
		AssignedAt: m.Timestamp, CreatedAt: m.Timestamp,
	}
	if cls == CategorySent {
		// Signals ProcessPotentialCompletion that the user sent this reply, so the task is reclassified as delegated rather than resolved.
		cm.RequesterCanonical = email
	}
	if !onThreadActivity(cm) {
		return false
	}
	_ = store.MarkAsProcessed(ctx, store.GetDB(), email, m.ID)
	return true
}

// Why: Separates the payload construction and side-effects (onThreadActivity callback) from the main AI analysis loop.
func buildGmailBatchPayload(email string, batchMsgs []types.RawMessage, classificationMap map[string]string) (string, map[string]types.RawMessage) {
	var sb strings.Builder
	msgMap := make(map[string]types.RawMessage)
	for _, m := range batchMsgs {
		msgMap[m.ID] = m
		metaStr := buildGmailMetadataString(m)
		senderField := m.Sender
		if m.SenderName != "" {
			senderField = fmt.Sprintf("%s <%s>", m.SenderName, m.Sender)
		}
		sb.WriteString(fmt.Sprintf("[ID:%s]%s F: %s\n%s\n---\n", m.ID, metaStr, senderField, m.Text))
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

func processGeminiItems(ctx context.Context, email string, user *store.User, aliases []string, items []store.TodoItem, classificationMap, toMap map[string]string, msgMap map[string]types.RawMessage) map[string]store.ConsolidatedMessage {
	result := make(map[string]store.ConsolidatedMessage, len(items))
	for _, item := range items {
		m, ok := msgMap[item.SourceTS]
		if !ok {
			item.SourceTS, m, ok = recoverSourceTS(item, msgMap)
		}
		if !ok {
			logger.Warnf("[GMAIL] mismatch SourceTS: %q dropped (task: %.80s)", item.SourceTS, item.Task)
			continue
		}
		params := services.TaskBuildParams{
			UserEmail:           email,
			User:                *user,
			Aliases:             aliases,
			Item:                item,
			SenderRaw:           m.Sender,
			ToHeader:            toMap[item.SourceTS],
			Source:              store.SourceGmail,
			Room:                "Gmail",
			Link:                fmt.Sprintf("https://mail.google.com/mail/u/0/#inbox/%s", item.SourceTS),
			SourceTS:            fmt.Sprintf("gmail-%s", item.SourceTS),
			Timestamp:           m.Timestamp,
			OriginalText:        m.Text,
			ThreadID:            m.ThreadID,
			RepliedToID:         m.ReplyToID,
			SourceChannels:      []string{store.SourceGmail},
			GmailClassification: classificationMap[item.SourceTS],
			IsCcOnly:            m.IsCcOnly,
		}
		result[item.SourceTS] = services.BuildTask(ctx, params)
	}
	return result
}

// recoverSourceTS re-anchors an item whose source_ts matches no batch message.
// Why: the model occasionally rewrites the [ID:...] tag (ISO timestamp, empty string);
// a single-message batch is still unambiguous, so recover instead of dropping the task.
func recoverSourceTS(item store.TodoItem, msgMap map[string]types.RawMessage) (string, types.RawMessage, bool) {
	if len(msgMap) != 1 {
		return item.SourceTS, types.RawMessage{}, false
	}
	for id, m := range msgMap {
		logger.Warnf("[GMAIL] recovered SourceTS %q -> %s (single-message batch)", item.SourceTS, id)
		return id, m, true
	}
	return item.SourceTS, types.RawMessage{}, false
}
