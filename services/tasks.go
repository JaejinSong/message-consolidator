package services

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"

	"google.golang.org/api/gmail/v1"
)

var (
	// genericOtherAssignees contains keywords that the AI might return for unspecific or group tasks.
	genericOtherAssignees = map[string]bool{"기타 업무": true, "기타업무": true, "other tasks": true, "미지정": true}

	// genericMeAssignees contains keywords that the AI might use to classify a task as belonging to the current user.
	genericMeAssignees = map[string]bool{"내 업무": true, "내업무": true, "my tasks": true, "mytasks": true, "나": true, "me": true}
)

// StripOriginalText removes the original text to reduce payload size.
func StripOriginalText(msgs []store.ConsolidatedMessage) {
	for i := range msgs {
		msgs[i].HasOriginal = msgs[i].OriginalText != ""
		msgs[i].OriginalText = ""
	}
}

// FormatMessagesForClient normalizes requesters and assignees, and flags user's tasks.
func FormatMessagesForClient(email string, msgs []store.ConsolidatedMessage) {
	user, _ := store.GetOrCreateUser(email, "", "")

	for i := range msgs {
		msgs[i].Requester = store.NormalizeName(email, msgs[i].Requester)
		msgs[i].Assignee = store.NormalizeName(email, msgs[i].Assignee)

		// Sanitize assignee names that might come from AI/JS as 'undefined' or 'unknown'.
		assignee := strings.TrimSpace(msgs[i].Assignee)
		if strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown") {
			msgs[i].Assignee = ""
			assignee = ""
		}

		if assignee == "" {
			continue
		}

		userName := strings.TrimSpace(user.Name)
		// A task is considered the user's if the assignee matches their name or is a generic "me" keyword.
		isMe := strings.EqualFold(assignee, userName) || strings.EqualFold(assignee, "me")

		if isMe {
			// Standardize the assignee to "me" for consistent frontend handling.
			msgs[i].Assignee = "me"
		} else {
			// [DEBUG] Track assignee mapping mismatches to help debug normalization rules.
			// Logged at DEBUG level to avoid flooding INFO logs.
			logger.Debugf("[ASSIGNEE_MAP] User: %s, Mismatched Assignee: '%s' (User.Name: '%s')",
				email, assignee, user.Name)
		}
	}
}

// ApplyTranslations fetches and applies translations for a batch of messages.
func ApplyTranslations(msgs []store.ConsolidatedMessage, lang string) {
	if lang == "" || len(msgs) == 0 {
		return
	}
	ids := make([]int, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	// Retrieve all translations in a single batch query for efficiency.
	translations, err := store.GetTaskTranslationsBatch(ids, lang)
	if err == nil {
		for i := range msgs {
			if t, ok := translations[msgs[i].ID]; ok {
				msgs[i].Task = t
			}
		}
	}
}

// PrepareMessagesForClient unifies translations, stripping, and formatting.
func PrepareMessagesForClient(email string, msgs []store.ConsolidatedMessage, lang string) {
	ApplyTranslations(msgs, lang)
	StripOriginalText(msgs)
	FormatMessagesForClient(email, msgs)
}

// HandleTaskCompletion orchestrates the process of marking a task as done,
// updating gamification stats, and potentially recording statistics for analytics.
func HandleTaskCompletion(email string, taskID int, done bool) (GamificationResult, error) {
	// Prevent duplicate rewards by checking the task's current state.
	msg, err := store.GetMessageByID(context.Background(), taskID)
	if err == nil && msg.Done && done {
		// If a task is already marked as done, skip the reward to prevent duplication.
		return GamificationResult{}, nil
	}

	if err := store.MarkMessageDone(email, taskID, done); err != nil {
		return GamificationResult{}, err
	}

	// Gamification rewards are only processed when a task is marked as 'done' (true).
	if !done {
		return GamificationResult{}, nil
	}

	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		return GamificationResult{}, err
	}

	return ProcessTaskCompletion(user)
}

// ReclassifyUserTasks re-evaluates assignees for a user's tasks based on identities and content.
func ReclassifyUserTasks(email string, user *store.User, aliases []string, msgs []store.ConsolidatedMessage) int {
	allMyIdentities := GetEffectiveAliases(*user, aliases)
	fixedCount := 0

	for _, m := range msgs {
		// If the assignee is a generic "other" keyword, clear it.
		if shouldClearAssignee(m.Assignee) {
			_ = store.UpdateTaskAssignee(email, m.ID, "")
			fixedCount++
			continue
		}

		isDirectGmail := IsDirectlyAddressedToMe(m, user.Email)
		isMarkedAsMine := IsAssigneeMarkedAsMine(m.Assignee, allMyIdentities)
		matchedByAlias := IsTaskMatchedByAlias(m, allMyIdentities, isDirectGmail)

		if isMarkedAsMine {
			// Special case for Gmail: if a task is generically assigned to "me" but the user
			// was not a direct recipient (e.g., they were on CC), un-assign it.
			if m.Source == "gmail" && !isDirectGmail {
				if isAssigneeGeneric(m.Assignee) {
					_ = store.UpdateTaskAssignee(email, m.ID, "")
					fixedCount++
					continue
				}
			}

			// Resolve the assignee to the user's preferred name or clear it if it's a generic "me".
			newAssignee, changed := resolveNewAssignee(user, m.Assignee, matchedByAlias)
			if changed {
				_ = store.UpdateTaskAssignee(email, m.ID, newAssignee)
				fixedCount++
			}
			continue
		}

		// If a task was unassigned but the content matches a user alias, assign it to the user.
		if matchedByAlias && strings.TrimSpace(m.Assignee) == "" {
			_ = store.UpdateTaskAssignee(email, m.ID, getPreferredName(user))
			fixedCount++
		}
	}
	return fixedCount
}

// RestoreGmailCCAssignment restores correct assignees for Gmail tasks where the user was only CC'd.
func RestoreGmailCCAssignment(ctx context.Context, email string, user *store.User, aliases []string, allMsgs []store.ConsolidatedMessage, svc *gmail.Service) int {
	fixedCount := 0
	for _, m := range allMsgs {
		if m.Source != "gmail" {
			continue
		}

		toHeader := extractToHeader(m.OriginalText)
		// If the user is in the "To" header and the task is unassigned, assign it to them.
		if isMeInToHeader(toHeader, user.Email) {
			if strings.TrimSpace(m.Assignee) == "" {
				_ = store.UpdateTaskAssignee(email, m.ID, getPreferredName(user))
				fixedCount++
			}
			continue
		}

		// If the task is wrongly assigned to the user (e.g., they were on CC),
		// try to find the actual assignee from the "To" header.
		if isWronglyAssignedToMe(m.Assignee, user, aliases) {
			actualAssignee := resolveActualAssignee(ctx, m, toHeader, svc)
			if actualAssignee != "" && strings.TrimSpace(m.Assignee) != actualAssignee {
				_ = store.UpdateTaskAssignee(email, m.ID, actualAssignee)
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
	// Include generic self-referential keywords.
	checkAliases := append([]string{"나", "me"}, aliases...)
	for _, a := range checkAliases {
		if a == "" {
			continue
		}
		textToCheck := m.OriginalText
		// Optimization for Gmail: If the user is a direct recipient, the AI-summarized `Task` text
		// is more likely to contain the relevant context for assignment than the full original email.
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

	// Check for explicit mentions like "@jjsong".
	if strings.Contains(textLower, "@"+aliasLower) {
		return true
	}
	// If the requester is not the alias itself (to avoid self-assignment on statements like "I will do X"),
	// check if the alias appears anywhere in the text.
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
func IsAssigneeMarkedAsMine(assignee string, identities []string) bool {
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
func IsDirectlyAddressedToMe(m store.ConsolidatedMessage, userEmail string) bool {
	if m.Source != "gmail" {
		return true
	}
	lowOrig := strings.ToLower(m.OriginalText)
	lowEmail := strings.ToLower(userEmail)

	toIdx := strings.Index(lowOrig, "to: ")
	if toIdx == -1 {
		return false
	}

	// Find the end of the "To:" block by looking for the start of the next header.
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

// resolveActualAssignee finds the true recipient of an email. It first tries to parse the
// "To" header from the raw text. If that fails, it makes a fallback API call to Gmail
// to fetch the message metadata and extract the "To" header directly.
func resolveActualAssignee(ctx context.Context, m store.ConsolidatedMessage, toHeader string, svc *gmail.Service) string {
	if toHeader != "" {
		return channels.ExtractNameFromEmail(toHeader)
	}
	// Fallback: If header isn't in the raw text, call the Gmail API.
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
				return channels.ExtractNameFromEmail(h.Value)
			}
		}
	}
	return ""
}
