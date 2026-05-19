package services

import (
	"context"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"

	"google.golang.org/api/gmail/v1"
)

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

// Why: Resolves the true primary recipient of an email by parsing the local "To" header or falling back to a Gmail API metadata request for precise correction of over-assigned tasks.
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
