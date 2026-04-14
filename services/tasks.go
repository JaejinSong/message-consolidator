package services

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/api/gmail/v1"
)

const (
	AssigneeShared    = "shared"
	CategoryPersonal  = "personal"
	CategoryShared    = "shared"
	CategoryRequested = "requested"
	CategoryOthers    = "others"
)

// BatchTranslateResult represents the status of a single task translation within a batch request.
type BatchTranslateResult struct {
	ID             int    `json:"id"`
	Success        bool   `json:"success"`
	TranslatedText string `json:"translated_text,omitempty"`
	Error          string `json:"error,omitempty"`
}

var (
	//Why: Defines keywords returned by the AI for unspecific or group tasks to support standardized unassignment logic.
	genericOtherAssignees = map[string]bool{"기타 업무": true, "기타업무": true, "other tasks": true, "미지정": true}

	//Why: Defines keywords used by the AI to classify a task as belonging to the current user, enabling uniform "__CURRENT_USER__" mapping.
	genericMeAssignees = map[string]bool{"내 업무": true, "내업무": true, "my tasks": true, "mytasks": true, "나": true, "me": true, "__current_user__": true}
)

// TasksService handles task-related operations including formatting, completion, and batch translation.

type TaskAI interface {
	GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error)
}

type TasksService struct {
	translationSvc *TranslationService
	geminiClient   TaskAI
}

func NewTasksService(trans *TranslationService, gemini TaskAI) *TasksService {
	return &TasksService{
		translationSvc: trans,
		geminiClient:   gemini,
	}
}

func (s *TasksService) GetTranslationService() *TranslationService {
	return s.translationSvc
}

// StripOriginalText removes the original text to reduce payload size.
func (s *TasksService) StripOriginalText(msgs []store.ConsolidatedMessage) {
	for i := range msgs {
		msgs[i].HasOriginal = msgs[i].OriginalText != ""
		msgs[i].OriginalText = ""
	}
}

func (s *TasksService) FormatMessagesForClient(ctx context.Context, email string, msgs []store.ConsolidatedMessage) {
	user, _ := store.GetOrCreateUser(ctx, email, "", "")
	
	// Pre-aggregation Phase: Extract all unique identifiers from message batch.
	// Why: Eliminates N+1 DB queries by resolving identities in a single bulk operation.
	identifiers := extractUniqueIdentifiers(msgs)
	aliasMap := store.BulkResolveAliases(ctx, email, identifiers)

	for i := range msgs {
		msgs[i].Requester = aliasMap[msgs[i].Requester]
		msgs[i].Assignee = aliasMap[msgs[i].Assignee]
		s.applyAssigneeRules(user, &msgs[i])
		s.assignCategory(email, user, &msgs[i])
	}
}

// assignCategory implements the server-side categorization priority logic.
// Priority: 1. personal, 2. shared, 3. requested, 4. others.
func (s *TasksService) assignCategory(email string, user *store.User, msg *store.ConsolidatedMessage) {
	// 1. personal: Assigned to 'me' or matches the user's preferred name
	if msg.Assignee == "me" {
		msg.Category = CategoryPersonal
		return
	}

	// 2. shared: Explicitly "shared" or contains group tags (@everyone, @channel, @here, etc.)
	if msg.Assignee == AssigneeShared || hasGroupMention(msg.Task) {
		msg.Category = CategoryShared
		return
	}

	// 3. requested: The current user is the requester but not the assignee
	if msg.Requester == email || (user.Name != "" && msg.Requester == user.Name) {
		msg.Category = CategoryRequested
		return
	}

	// 4. others: Supporting message or non-actionable reference
	msg.Category = CategoryOthers
}

// hasGroupMention detects common team/group tags to identify non-individual tasks.
func hasGroupMention(text string) bool {
	content := strings.ToLower(text)
	groupWords := []string{"@everyone", "@channel", "@here", "team", "everyone"}
	for _, word := range groupWords {
		if strings.Contains(content, word) {
			return true
		}
	}
	return false
}

func extractUniqueIdentifiers(msgs []store.ConsolidatedMessage) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, m := range msgs {
		if !seen[m.Requester] && m.Requester != "" {
			ids = append(ids, m.Requester)
			seen[m.Requester] = true
		}
		if !seen[m.Assignee] && m.Assignee != "" {
			ids = append(ids, m.Assignee)
			seen[m.Assignee] = true
		}
	}
	return ids
}

func (s *TasksService) applyAssigneeRules(user *store.User, msg *store.ConsolidatedMessage) {
	assignee := strings.TrimSpace(msg.Assignee)
	isUnknown := strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown")
	if isUnknown || assignee == "" {
		msg.Assignee = ""
		return
	}

	userName := strings.TrimSpace(user.Name)
	isMe := strings.EqualFold(assignee, userName) || strings.EqualFold(assignee, "me") || strings.EqualFold(assignee, "__current_user__")
	if isMe {
		msg.Assignee = "me"
	}
}

// ApplyTranslations fetches cached translations and triggers JIT for missing ones.
// Why: Returns English immediately for missing translations to prevent UI blocking.
func (s *TasksService) ApplyTranslations(ctx context.Context, email, lang string, msgs []store.ConsolidatedMessage) {
	if lang == "" || strings.EqualFold(lang, "en") || len(msgs) == 0 {
		return
	}
	ids := make([]int, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	translations, _ := store.GetTaskTranslationsBatch(ctx, ids, lang)
	var missingIDs []int
	for i := range msgs {
		if t, ok := translations[msgs[i].ID]; ok {
			msgs[i].Task = t
		} else {
			missingIDs = append(missingIDs, msgs[i].ID)
		}
	}
	s.triggerJITTranslation(email, lang, missingIDs)
}

func (s *TasksService) triggerJITTranslation(email, lang string, ids []int) {
	if len(ids) == 0 {
		return
	}
	// Why: Asynchronously triggers JIT translation to avoid blocking the main data request.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, _ = s.ProcessBatchTranslation(ctx, email, ids, lang)
	}()
}

// PrepareMessagesForClient unifies translations, stripping, and formatting.
func (s *TasksService) PrepareMessagesForClient(ctx context.Context, email string, msgs []store.ConsolidatedMessage, lang string) {
	s.ApplyTranslations(ctx, email, lang, msgs)
	s.StripOriginalText(msgs)
	s.FormatMessagesForClient(ctx, email, msgs)
}

// HandleTaskCompletion orchestrates the process of marking a task as done.
func (s *TasksService) HandleTaskCompletion(ctx context.Context, email string, taskID int, done bool) error {
	if taskID <= 0 {
		return fmt.Errorf("invalid task id: %d", taskID)
	}
	msg, err := store.GetMessageByID(ctx, store.GetDB(), email, taskID)
	if err == nil && msg.Done && done {
		return nil
	}

	return store.MarkMessageDone(ctx, store.GetDB(), email, taskID, done)
}

// ReclassifyUserTasks re-evaluates assignees for a user's tasks based on identities and content.
func (s *TasksService) ReclassifyUserTasks(ctx context.Context, email string, user *store.User, aliases []string, msgs []store.ConsolidatedMessage) int {
	allMyIdentities := GetEffectiveAliases(*user, aliases)
	fixedCount := 0

	for _, m := range msgs {
		if s.reclassifySingleTask(ctx, email, user, allMyIdentities, m) {
			fixedCount++
		}
	}
	return fixedCount
}

func (s *TasksService) reclassifySingleTask(ctx context.Context, email string, user *store.User, allMyIdentities []string, m store.ConsolidatedMessage) bool {
	// Guard: Clear generic "other" assignees for manual re-assignment.
	if shouldClearAssignee(m.Assignee) {
		_ = store.UpdateTaskAssignee(ctx, nil, email, m.ID, "")
		return true
	}

	isMarkedAsMine := s.IsAssigneeMarkedAsMine(m.Assignee, allMyIdentities)
	if !isMarkedAsMine {
		return false
	}

	isDirectGmail := s.IsDirectlyAddressedToMe(m, user.Email)
	
	// Guard: Automatically un-assign Gmail tasks wrongly assigned to "me" if only CC/BCC.
	if m.Source == "gmail" && !isDirectGmail && isAssigneeGeneric(m.Assignee) {
		_ = store.UpdateTaskAssignee(ctx, nil, email, m.ID, "")
		return true
	}

	matchedByAlias := IsTaskMatchedByAlias(m, allMyIdentities, isDirectGmail)
	newAssignee, changed := s.resolveNewAssignee(user, m.Assignee, matchedByAlias)
	if changed {
		_ = store.UpdateTaskAssignee(ctx, nil, email, m.ID, newAssignee)
		return true
	}

	return false
}

// RestoreGmailCCAssignment identifies Gmail tasks that were incorrectly assigned due to the user being CC'd.
// Why: [Performance] Uses errgroup with a worker limit of 20 to parallelize Gmail API resolution and returns a map for batch DB updates.
func (s *TasksService) RestoreGmailCCAssignment(ctx context.Context, email string, user *store.User, aliases []string, msgs []store.ConsolidatedMessage, svc *gmail.Service) (map[int]string, int) {
	updates := make(map[int]string)
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(20)

	for _, m := range msgs {
		m := m
		g.Go(func() error {
			id, actual, changed := s.checkRestoreGmailCC(ctx, email, user, aliases, m, svc)
			if changed {
				mu.Lock()
				updates[id] = actual
				mu.Unlock()
			}
			return nil
		})
	}

	_ = g.Wait()
	return updates, len(updates)
}

func (s *TasksService) checkRestoreGmailCC(ctx context.Context, email string, user *store.User, aliases []string, m store.ConsolidatedMessage, svc *gmail.Service) (int, string, bool) {
	if m.Source != "gmail" {
		return 0, "", false
	}

	toHeader := extractToHeader(m.OriginalText)
	if isMeInToHeader(toHeader, user.Email) {
		return 0, "", false
	}

	if !isWronglyAssignedToMe(m.Assignee, user, aliases) {
		return 0, "", false
	}

	actualAssignee := resolveActualAssignee(ctx, m, toHeader, svc)
	if actualAssignee == "" || strings.TrimSpace(m.Assignee) == actualAssignee {
		return 0, "", false
	}

	return m.ID, actualAssignee, true
}

func (s *TasksService) restoreSingleGmailCC(ctx context.Context, email string, user *store.User, aliases []string, m store.ConsolidatedMessage, svc *gmail.Service) bool {
	id, actual, changed := s.checkRestoreGmailCC(ctx, email, user, aliases, m, svc)
	if changed {
		_ = store.UpdateTaskAssignee(ctx, nil, email, id, actual)
		return true
	}
	return false
}

// Logic Helpers

// GetEffectiveAliases combines the user's primary name and their registered aliases into a single list.
func GetEffectiveAliases(user store.User, aliases []string) []string {
	var all []string
	if user.Name != "" {
		all = append(all, user.Name)
	}
	all = append(all, aliases...)
	return all
}

// IsTaskMatchedByAlias checks if the task content or requester matches any of the user's identities.
func IsTaskMatchedByAlias(m store.ConsolidatedMessage, aliases []string, isDirectGmail bool) bool {
	// Explicit group mentions should not be auto-assigned to individuals
	if hasGroupMention(m.Task) {
		return false
	}

	//Why: Augmented alias list with generic self-referential keywords ("나", "me") to broaden task matching coverage.
	checkAliases := append([]string{"나", "me"}, aliases...)
	for _, a := range checkAliases {
		if a == "" {
			continue
		}
		textToCheck := m.OriginalText
		//Why: Optimizes Gmail matching by favoring the AI-summarized Task text over the full original email when the user is a direct recipient, reducing noise.
		if m.Source == "gmail" && isDirectGmail {
			textToCheck = m.Task
		}
		if IsAliasMatched(textToCheck, m.Requester, a) {
			return true
		}
	}
	return false
}

// IsAliasMatched performs the core matching logic for an alias within a text.
func IsAliasMatched(text, requester, alias string) bool {
	if alias == "" || text == "" {
		return false
	}
	textLower := strings.ToLower(text)
	aliasLower := strings.ToLower(alias)

	//Why: Specifically checks for explicit @-mentions to provide high-confidence task identification.
	if strings.Contains(textLower, "@"+aliasLower) {
		return true
	}
	//Why: Prevents accidental self-assignment by verifying that the user is not the requester before matching their alias within the message body.
	if !strings.EqualFold(requester, alias) {
		if strings.Contains(textLower, aliasLower) {
			return true
		}
	}
	return false
}

// shouldClearAssignee checks if an assignee name is a generic "other" keyword.
func shouldClearAssignee(assignee string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	return genericOtherAssignees[norm]
}

// isAssigneeGeneric checks if an assignee is either empty or a generic "me" keyword.
func isAssigneeGeneric(assignee string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	return norm == "" || genericMeAssignees[norm]
}

// IsAssigneeMarkedAsMine checks if the assignee matches any of the user's known identities or generic "me" keywords.
func (s *TasksService) IsAssigneeMarkedAsMine(assignee string, identities []string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	if genericMeAssignees[norm] {
		return true
	}
	for _, a := range identities {
		if a != "" && strings.EqualFold(assignee, a) {
			return true
		}
	}
	return false
}

// IsDirectlyAddressedToMe parses the raw email text to determine if the user's email
// is in the "To:" header field, as opposed to CC or BCC.
func (s *TasksService) IsDirectlyAddressedToMe(m store.ConsolidatedMessage, userEmail string) bool {
	if m.Source != "gmail" {
		return true
	}
	lowOrig := strings.ToLower(m.OriginalText)
	lowEmail := strings.ToLower(userEmail)

	toIdx := strings.Index(lowOrig, "to: ")
	if toIdx == -1 {
		return false
	}

	//Why: Identifies the boundaries of the "To:" block by locating the next standard email header to avoid matching emails in CC or BCC fields.
	limitIdx := findHeaderEnd(lowOrig, toIdx)
	toBlock := ""
	if limitIdx != -1 && limitIdx > toIdx {
		toBlock = lowOrig[toIdx:limitIdx]
	} else {
		toBlock = lowOrig[toIdx:]
	}
	return strings.Contains(toBlock, lowEmail)
}

// findHeaderEnd finds the starting position of the next email header after a given point.
func findHeaderEnd(text string, start int) int {
	headers := []string{"cc: ", "bcc: ", "subject: "}
	minIdx := -1
	for _, h := range headers {
		idx := strings.Index(text[start:], h)
		if idx != -1 {
			absIdx := start + idx
			if minIdx == -1 || absIdx < minIdx {
				minIdx = absIdx
			}
		}
	}
	return minIdx
}

// resolveNewAssignee determines the correct assignee name or clears it.
func (s *TasksService) resolveNewAssignee(user *store.User, current string, matchedByAlias bool) (string, bool) {
	if matchedByAlias {
		name := getPreferredName(user)
		return name, current != name
	}
	lowCurr := strings.ToLower(current)
	if genericMeAssignees[lowCurr] {
		// Instead of clearing or using me, mark as shared if it was a generic "me" but didn't match an alias
		return AssigneeShared, current != AssigneeShared
	}
	return current, false
}

// getPreferredName returns the user's display name if available, otherwise their email.
func getPreferredName(user *store.User) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Email
}

// extractToHeader extracts the content of the "To: " header from raw email text.
func extractToHeader(text string) string {
	toIdx := strings.Index(text, "To: ")
	subjIdx := strings.Index(text, ", Subject: ")
	if toIdx != -1 && subjIdx != -1 && subjIdx > toIdx {
		return text[toIdx+4 : subjIdx]
	}
	return ""
}

// isMeInToHeader checks if a given email address is present in a header string.
func isMeInToHeader(header, email string) bool {
	return header != "" && strings.Contains(strings.ToLower(header), strings.ToLower(email))
}

// isWronglyAssignedToMe checks if a task is assigned to the current user, including all their aliases.
func isWronglyAssignedToMe(assignee string, user *store.User, aliases []string) bool {
	lower := strings.ToLower(strings.TrimSpace(assignee))
	if lower == "" || genericMeAssignees[lower] || strings.EqualFold(assignee, user.Name) || strings.EqualFold(assignee, user.Email) {
		return true
	}
	for _, a := range aliases {
		if a != "" && strings.EqualFold(assignee, a) {
			return true
		}
	}
	return false
}

//Why: Resolves the true primary recipient of an email by parsing the local "To" header or falling back to a Gmail API metadata request for precise correction of over-assigned tasks.
func resolveActualAssignee(ctx context.Context, m store.ConsolidatedMessage, toHeader string, svc *gmail.Service) string {
	if toHeader != "" {
		return types.ExtractNameFromEmail(toHeader)
	}
	//Why: Fallback mechanism: Retrieves the "To" header via a direct Gmail API metadata request if it is missing from the stored message context.
	msgID := m.SourceTS
	if strings.HasPrefix(msgID, "gmail-") {
		parts := strings.Split(msgID, "-")
		if len(parts) >= 2 {
			msgID = parts[1]
		}
	}

	msg, err := svc.Users.Messages.Get("me", msgID).Format("metadata").MetadataHeaders("To").Context(ctx).Do()
	if err == nil && msg.Payload != nil {
		for _, h := range msg.Payload.Headers {
			if h.Name == "To" {
				return types.ExtractNameFromEmail(h.Value)
			}
		}
	}
	return ""
}

// ProcessBatchTranslation handles multiple task translation requests in an optimized single batch.
// Why: Implements Page-unit Pure JIT pattern to eliminate N+1 AI calls.
func (s *TasksService) ProcessBatchTranslation(ctx context.Context, email string, taskIDs []int, lang string) ([]BatchTranslateResult, error) {
	if s.translationSvc == nil { return nil, fmt.Errorf("service not ready") }
	
	cached, _ := store.GetTaskTranslationsBatch(ctx, taskIDs, lang)
	missingIDs := s.getMissingIDs(taskIDs, cached)
	
	newTrans := make(map[int]string)
	if len(missingIDs) > 0 {
		var err error
		newTrans, err = s.executeBatchTranslation(ctx, email, missingIDs, lang)
		if err != nil { logger.Errorf("[TASKS] Batch failed: %v", err) }
	}

	return s.mergeBatchResults(taskIDs, cached, newTrans), nil
}

func (s *TasksService) getMissingIDs(all []int, cached map[int]string) []int {
	var missing []int
	for _, id := range all {
		if _, ok := cached[id]; !ok { missing = append(missing, id) }
	}
	return missing
}

func (s *TasksService) executeBatchTranslation(ctx context.Context, email string, ids []int, lang string) (map[int]string, error) {
	reqs := s.prepareTranslateRequests(ctx, email, ids)
	if len(reqs) == 0 { return nil, nil }

	results, err := s.translationSvc.TranslateBatch(ctx, email, reqs, lang)
	if err != nil { return nil, err }

	batchMap := make(map[int]string)
	for _, r := range results {
		if r.Error == "" {
			batchMap[r.MessageID] = r.Text
		}
	}
	
	_ = store.SaveTaskTranslationsBulk(ctx, lang, batchMap)
	return batchMap, nil
}

func (s *TasksService) prepareTranslateRequests(ctx context.Context, email string, ids []int) []store.TranslateRequest {
	var reqs []store.TranslateRequest
	for _, id := range ids {
		msg, err := store.GetMessageByID(ctx, store.GetDB(), email, id)
		if err != nil { continue }
		reqs = append(reqs, store.TranslateRequest{ID: id, Text: msg.Task})
	}
	return reqs
}

func (s *TasksService) mergeBatchResults(ids []int, cached, newTrans map[int]string) []BatchTranslateResult {
	final := make([]BatchTranslateResult, len(ids))
	for i, id := range ids {
		text, ok := cached[id]
		if !ok { text = newTrans[id] }
		
		success := text != ""
		final[i] = BatchTranslateResult{ID: id, Success: success, TranslatedText: text}
		if !success { final[i].Error = "translation missing" }
	}
	return final
}

// MergeTasks consolidates multiple tasks into one using AI summarization for the title.
// Why: [Contextual Merge] Generates a representative English title from all merged messages.
func (s *TasksService) MergeTasks(ctx context.Context, email string, targetIDs []int64, destID int64) error {
	if destID <= 0 || len(targetIDs) == 0 {
		return fmt.Errorf("invalid merge parameters: targetCount=%d, destID=%d", len(targetIDs), destID)
	}
	allIDs := append(targetIDs, destID)
	msgs, err := store.GetMessagesByIDs(ctx, store.GetDB(), email, s.toIntSlice(allIDs))
	if err != nil { return err }

	var dest *store.ConsolidatedMessage
	var sources []store.ConsolidatedMessage
	for i := range msgs {
		if int64(msgs[i].ID) == destID { dest = &msgs[i] } else { sources = append(sources, msgs[i]) }
	}
	if dest == nil { return fmt.Errorf("destination task not found") }

	// Why: [Reliability] AI summary is progressive; failures fallback to existing title.
	newTitle := s.generateSummaryTitle(ctx, email, dest, sources)
	return store.MergeTasksWithTitle(ctx, email, targetIDs, destID, newTitle)
}

func (s *TasksService) toIntSlice(ids []int64) []int {
	res := make([]int, len(ids))
	for i, id := range ids { res[i] = int(id) }
	return res
}

func (s *TasksService) generateSummaryTitle(ctx context.Context, email string, dest *store.ConsolidatedMessage, sources []store.ConsolidatedMessage) string {
	type summaryInput struct { Title string `json:"title"`; Text string `json:"original_txt"` }
	inputs := make([]summaryInput, 0, len(sources)+1)
	inputs = append(inputs, summaryInput{Title: dest.Task, Text: dest.OriginalText})
	for _, src := range sources {
		inputs = append(inputs, summaryInput{Title: src.Task, Text: src.OriginalText})
	}

	data, err := json.Marshal(inputs)
	if err != nil { return dest.Task } // Fallback

	title, err := s.geminiClient.GenerateMergedTaskTitle(ctx, email, string(data))
	if err != nil || title == "" {
		logger.Errorf("AI Merge Summary Failed: %v (fallback to: %s)", err, dest.Task)
		return dest.Task
	}
	return title
}
