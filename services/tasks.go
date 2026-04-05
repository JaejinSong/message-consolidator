package services

import (
	"context"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
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

type TasksService struct {
	translationSvc *TranslationService
}

func NewTasksService(trans *TranslationService) *TasksService {
	return &TasksService{
		translationSvc: trans,
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

// FormatMessagesForClient normalizes requesters and assignees, and flags user's tasks.
func (s *TasksService) FormatMessagesForClient(ctx context.Context, email string, msgs []store.ConsolidatedMessage) {
	user, _ := store.GetOrCreateUser(ctx, email, "", "")

	for i := range msgs {
		msgs[i].Requester = store.NormalizeName(email, msgs[i].Requester)
		msgs[i].Assignee = store.NormalizeName(email, msgs[i].Assignee)

		assignee := strings.TrimSpace(msgs[i].Assignee)
		if strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown") {
			msgs[i].Assignee = ""
			assignee = ""
		}

		if assignee == "" {
			continue
		}

		userName := strings.TrimSpace(user.Name)
		isMe := strings.EqualFold(assignee, userName) || strings.EqualFold(assignee, "me") || strings.EqualFold(assignee, "__current_user__")

		if isMe {
			msgs[i].Assignee = "me"
		}
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
func (s *TasksService) HandleTaskCompletion(ctx context.Context, email string, taskID int, done bool) (GamificationResult, error) {
	msg, err := store.GetMessageByID(ctx, email, taskID)
	if err == nil && msg.Done && done {
		return GamificationResult{}, nil
	}

	if err := store.MarkMessageDone(ctx, email, taskID, done); err != nil {
		return GamificationResult{}, err
	}

	//Why: Restricts gamification reward processing to transitions where a task is explicitly being marked as completed.
	if !done {
		return GamificationResult{}, nil
	}

	user, err := store.GetOrCreateUser(ctx, email, "", "")
	if err != nil {
		return GamificationResult{}, err
	}

	return ProcessTaskCompletion(ctx, user)
}

// ReclassifyUserTasks re-evaluates assignees for a user's tasks based on identities and content.
func (s *TasksService) ReclassifyUserTasks(ctx context.Context, email string, user *store.User, aliases []string, msgs []store.ConsolidatedMessage) int {
	allMyIdentities := GetEffectiveAliases(*user, aliases)
	fixedCount := 0

	for _, m := range msgs {
		//Why: Clears generic "other" assignees to keep the task pool clean and allow for manual re-assignment.
		if shouldClearAssignee(m.Assignee) {
			_ = store.UpdateTaskAssignee(ctx, email, m.ID, "")
			fixedCount++
			continue
		}

		isDirectGmail := s.IsDirectlyAddressedToMe(m, user.Email)
		isMarkedAsMine := s.IsAssigneeMarkedAsMine(m.Assignee, allMyIdentities)
		matchedByAlias := IsTaskMatchedByAlias(m, allMyIdentities, isDirectGmail)

		if isMarkedAsMine {
			//Why: Automatically un-assigns Gmail tasks that were generically assigned to "me" if the user was only a CC/BCC recipient, correcting AI over-assignment.
			if m.Source == "gmail" && !isDirectGmail {
				if isAssigneeGeneric(m.Assignee) {
					_ = store.UpdateTaskAssignee(ctx, email, m.ID, "")
					fixedCount++
					continue
				}
			}

			//Why: Resolves generic "me" assignees to the user's preferred display name for consistency in the UI and database.
			newAssignee, changed := resolveNewAssignee(user, m.Assignee, matchedByAlias)
			if changed {
				_ = store.UpdateTaskAssignee(ctx, email, m.ID, newAssignee)
				fixedCount++
			}
			continue
		}

		//Why: Proactively assigns unassigned tasks to the user if the message content explicitly matches one of their registered aliases or mentions.
		if matchedByAlias && strings.TrimSpace(m.Assignee) == "" {
			_ = store.UpdateTaskAssignee(ctx, email, m.ID, getPreferredName(user))
			fixedCount++
		}
	}
	return fixedCount
}

// RestoreGmailCCAssignment restores correct assignees for Gmail tasks where the user was only CC'd.
func (s *TasksService) RestoreGmailCCAssignment(ctx context.Context, email string, user *store.User, aliases []string, allMsgs []store.ConsolidatedMessage, svc *gmail.Service) int {
	fixedCount := 0
	for _, m := range allMsgs {
		if m.Source != "gmail" {
			continue
		}

		toHeader := extractToHeader(m.OriginalText)
		//Why: Assigns unassigned Gmail tasks to the user if they were a direct recipient in the "To" header, providing high-confidence auto-assignment.
		if isMeInToHeader(toHeader, user.Email) {
			if strings.TrimSpace(m.Assignee) == "" {
				_ = store.UpdateTaskAssignee(ctx, email, m.ID, getPreferredName(user))
				fixedCount++
			}
			continue
		}

		//Why: Corrects accidental assignments to the current user for CC'd emails by attempting to resolve the actual primary recipient from the "To" header.
		if isWronglyAssignedToMe(m.Assignee, user, aliases) {
			actualAssignee := resolveActualAssignee(ctx, m, toHeader, svc)
			if actualAssignee != "" && strings.TrimSpace(m.Assignee) != actualAssignee {
				_ = store.UpdateTaskAssignee(ctx, email, m.ID, actualAssignee)
				fixedCount++
			}
		}
	}
	return fixedCount
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
func resolveNewAssignee(user *store.User, current string, matchedByAlias bool) (string, bool) {
	if matchedByAlias {
		name := getPreferredName(user)
		return name, current != name
	}
	lowCurr := strings.ToLower(current)
	if genericMeAssignees[lowCurr] {
		return "", true
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
		msg, err := store.GetMessageByID(ctx, email, id)
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
