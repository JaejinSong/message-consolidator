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

	aliases, _ := store.GetUserAliasesByEmail(ctx, email)
	identities := GetEffectiveAliases(*user, aliases)

	for i := range msgs {
		if resolved := aliasMap[msgs[i].Requester]; resolved != "" {
			msgs[i].Requester = resolved
		}
		if resolved := aliasMap[msgs[i].Assignee]; resolved != "" {
			msgs[i].Assignee = resolved
		}
		s.applyAssigneeRules(user, identities, &msgs[i])
		s.assignCategory(user, identities, &msgs[i])
	}
}

// assignCategory implements the server-side categorization priority logic.
// Priority: 1. personal, 2. shared, 3. requested, 4. others.
func (s *TasksService) assignCategory(user *store.User, identities []string, msg *store.ConsolidatedMessage) {
	if s.IsAssigneeMarkedAsMine(msg.Assignee, identities) || strings.EqualFold(msg.AssigneeCanonical, user.Email) {
		msg.Category = CategoryPersonal
		return
	}
	if msg.Assignee == AssigneeShared || hasGroupMention(msg.Task) {
		msg.Category = CategoryShared
		return
	}
	// Why: RequesterCanonical is the canonical email unaffected by alias resolution.
	// msg.Requester == user.Email is a fallback for legacy records without RequesterCanonical.
	if strings.EqualFold(msg.RequesterCanonical, user.Email) || msg.Requester == user.Email {
		msg.Category = CategoryRequested
		return
	}
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

func (s *TasksService) applyAssigneeRules(user *store.User, identities []string, msg *store.ConsolidatedMessage) {
	assignee := strings.TrimSpace(msg.Assignee)
	isUnknown := strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown")
	if isUnknown || assignee == "" {
		msg.Assignee = ""
		return
	}

	if s.IsAssigneeMarkedAsMine(assignee, identities) || strings.EqualFold(msg.AssigneeCanonical, user.Email) {
		msg.Assignee = user.PreferredName()
	}

	if strings.EqualFold(strings.TrimSpace(msg.Requester), user.Email) || s.IsAssigneeMarkedAsMine(msg.Requester, identities) || strings.EqualFold(msg.RequesterCanonical, user.Email) {
		msg.RequesterCanonical = user.Email
		msg.Requester = user.PreferredName()
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
		if raw, ok := translations[msgs[i].ID]; ok {
			mainTask, subTexts := parseTranslatedText(raw)
			msgs[i].Task = mainTask
			if len(subTexts) > 0 && len(msgs[i].Subtasks) == len(subTexts) {
				for j := range msgs[i].Subtasks {
					msgs[i].Subtasks[j].Task = subTexts[j]
				}
			}
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

	if !s.IsAssigneeMarkedAsMine(m.Assignee, GetEffectiveAliases(*user, aliases)) {
		return 0, "", false
	}

	actualAssignee := resolveActualAssignee(ctx, m, toHeader, svc)
	if actualAssignee == "" || strings.TrimSpace(m.Assignee) == actualAssignee {
		return 0, "", false
	}

	return m.ID, actualAssignee, true
}


// Logic Helpers

// GetEffectiveAliases combines the user's primary name, email, email prefix, and registered aliases.
func GetEffectiveAliases(user store.User, aliases []string) []string {
	seen := make(map[string]bool)
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
		}
	}
	add(user.Name)
	add(user.Email)
	if prefix, _, found := strings.Cut(user.Email, "@"); found {
		add(prefix)
	}
	for _, a := range aliases {
		add(a)
	}
	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	return result
}

// IsTaskMatchedByAlias checks if the task content or requester matches any of the user's identities.
func IsTaskMatchedByAlias(m store.ConsolidatedMessage, aliases []string, isDirectGmail bool) bool {
	// Explicit group mentions should not be auto-assigned to individuals
	if hasGroupMention(m.Task) {
		return false
	}

	checkAliases := aliases
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

// isAssigneeGeneric checks if an assignee is either empty or a self-referential AI token.
func isAssigneeGeneric(assignee string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	return norm == "" || store.IsSelfAssigneeToken(norm)
}

// IsAssigneeMarkedAsMine checks if the assignee matches any of the user's known identities.
// store.IsSelfAssigneeToken handles backward-compat for legacy "me" records in DB.
// Both sides are normalized so suffix variants like "Jaejin Song (JJ)" match "Jaejin Song".
func (s *TasksService) IsAssigneeMarkedAsMine(assignee string, identities []string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	if store.IsSelfAssigneeToken(norm) {
		return true
	}
	normalizedAssignee := store.NormalizeIdentifier(assignee)
	for _, a := range identities {
		if a != "" && (strings.EqualFold(assignee, a) || strings.EqualFold(normalizedAssignee, store.NormalizeIdentifier(a))) {
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

	toIdx := strings.Index(lowOrig, "t: ")
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
// OriginalText uses abbreviated headers: "T: ", "C: ", "S: ", "B: " separated by newlines.
func findHeaderEnd(text string, start int) int {
	headers := []string{"\nc: ", "\ns: ", "\nb: "}
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
		name := user.PreferredName()
		return name, current != name
	}
	lowCurr := strings.ToLower(current)
	if store.IsSelfAssigneeToken(lowCurr) {
		// Self-token without a matched alias: treat as shared broadcast.
		return AssigneeShared, current != AssigneeShared
	}
	return current, false
}


// extractToHeader extracts the content of the "T: " header from raw email text.
// OriginalText format: "T: <to>\nC: <cc>\nS: <subject>\nB:\n<body>"
func extractToHeader(text string) string {
	toIdx := strings.Index(text, "T: ")
	if toIdx == -1 {
		return ""
	}
	endIdx := strings.Index(text[toIdx:], "\n")
	if endIdx == -1 {
		return text[toIdx+3:]
	}
	return text[toIdx+3 : toIdx+endIdx]
}

// isMeInToHeader checks if a given email address is present in a header string.
func isMeInToHeader(header, email string) bool {
	return header != "" && strings.Contains(strings.ToLower(header), strings.ToLower(email))
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
		reqs = append(reqs, BuildTranslateRequest(id, msg.Task, msg.Subtasks))
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
	inputs := []struct{ Title, Text string }{{dest.Task, dest.OriginalText}}
	for _, src := range sources {
		inputs = append(inputs, struct{ Title, Text string }{src.Task, src.OriginalText})
	}

	data, _ := json.Marshal(inputs)
	title, err := s.geminiClient.GenerateMergedTaskTitle(ctx, email, string(data))
	if err == nil && title != "" { return title }

	// Why: [Reliability] AI response blocks/timeouts fallback to simple title concatenation to prevent info loss.
	logger.Warnf("[TASKS] AI Merge Summary Failed (Error: %v). Falling back to concatenation.", err)
	titles := make([]string, 0, len(sources)+1)
	titles = append(titles, dest.Task)
	for _, src := range sources {
		titles = append(titles, src.Task)
	}
	return s.truncateTitle(strings.Join(titles, " | "), 250)
}

func (s *TasksService) truncateTitle(t string, max int) string {
	if len(t) <= max {
		return t
	}
	return t[:max-3] + "..."
}

// ResolveProposals resolves extraction results against current active tasks.
// AI proposes (rawItems), Backend decides (returns refined items with correct IDs/States).
func (s *TasksService) ResolveProposals(ctx context.Context, email, room string, rawItems []store.TodoItem, active []store.ConsolidatedMessage) []store.TodoItem {
	var results []store.TodoItem
	for _, item := range rawItems {
		match := s.findMatch(room, item, active)
		if match != nil {
			item.ID = &match.ID
			// Upgrade 'new' to 'update' if we found an existing task.
			// Keep 'resolve', 'cancel', 'update' as AI intended.
			if item.State == "new" {
				item.State = "update"
			}
		} else {
			// Logic: If no match found, states requiring an ID must be downgraded.
			if item.State == "update" || item.State == "resolve" || item.State == "cancel" {
				if item.Task != "" && item.State == "update" {
					item.State = "new" // Only 'update' can safely downgrade to 'new'
				} else {
					item.State = "none" // resolve/cancel with no match is dropped
				}
			}
		}
		results = append(results, item)
	}
	return results
}

// translationPayload is the JSON structure stored in task_translations.translated_text
// when subtasks are present. Plain strings (legacy) are still supported.
type translationPayload struct {
	T string   `json:"t"`
	S []string `json:"s,omitempty"`
}

// BuildTranslateRequest encodes task + subtask texts into a single TranslateRequest.
// The Text field uses JSON when subtasks exist so the translator receives structured content.
func BuildTranslateRequest(id int, task string, subtasks []store.Subtask) store.TranslateRequest {
	if len(subtasks) == 0 {
		return store.TranslateRequest{ID: id, Text: task}
	}
	subs := make([]string, len(subtasks))
	for i, s := range subtasks {
		subs[i] = s.Task
	}
	p := translationPayload{T: task, S: subs}
	b, err := json.Marshal(p)
	if err != nil {
		return store.TranslateRequest{ID: id, Text: task}
	}
	return store.TranslateRequest{ID: id, Text: string(b)}
}

// parseTranslatedText parses a stored translated_text value (plain or JSON).
// Returns (mainTask, subtaskTexts).
func parseTranslatedText(raw string) (string, []string) {
	if len(raw) == 0 || raw[0] != '{' {
		return raw, nil
	}
	var p translationPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return raw, nil
	}
	return p.T, p.S
}

func (s *TasksService) findMatch(room string, item store.TodoItem, active []store.ConsolidatedMessage) *store.ConsolidatedMessage {
	// ID-first: AI explicitly identified the target task from existing context.
	if item.ID != nil && *item.ID != 0 {
		for i := range active {
			if active[i].ID == *item.ID {
				return &active[i]
			}
		}
	}

	for i := range active {
		m := &active[i]
		if m.Room != room || m.Category != item.Category { continue }

		sim := store.CalculateSimilarity(item.Task, m.Task)
		if sim >= 0.80 { return m }

		// Affinity Group Bonus: If AI group matches, we are more lenient (threshold 0.5)
		if item.AffinityGroupID != "" && len(m.Metadata) > 0 {
			var meta map[string]interface{}
			if err := json.Unmarshal(m.Metadata, &meta); err == nil {
				if gid, ok := meta["affinity_group_id"].(string); ok && gid == item.AffinityGroupID {
					if sim >= 0.50 { return m }
				}
			}
		}
	}
	return nil
}
