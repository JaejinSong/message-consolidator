package services

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/store"
	"strings"

	"google.golang.org/api/gmail/v1"
)

var (
	// AI가 담당자를 불특정 다수로 분류했을 때 반환하는 키워드 목록
	genericOtherAssignees = map[string]bool{"기타 업무": true, "기타업무": true, "other tasks": true, "미지정": true}

	// AI가 담당자를 나 자신으로 분류했을 때 반환하는 키워드 목록
	genericMeAssignees = map[string]bool{"내 업무": true, "내업무": true, "my tasks": true, "mytasks": true, "나": true, "me": true}
)

// HandleTaskCompletion orchestrates the process of marking a task as done,
// updating gamification stats, and potentially recording statistics for analytics.
func HandleTaskCompletion(email string, taskID int, done bool) (GamificationResult, error) {
	// 중복 보상 방지: 현재 상태 확인
	msg, err := store.GetMessageByID(context.Background(), taskID)
	if err == nil && msg.Done && done {
		// 이미 완료된 업무를 다시 완료로 표시하는 경우 보상 생략
		return GamificationResult{}, nil
	}

	if err := store.MarkMessageDone(email, taskID, done); err != nil {
		return GamificationResult{}, err
	}

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
		if shouldClearAssignee(m.Assignee) {
			_ = store.UpdateTaskAssignee(email, m.ID, "")
			fixedCount++
			continue
		}

		isDirectGmail := IsDirectlyAddressedToMe(m, user.Email)
		isMarkedAsMine := IsAssigneeMarkedAsMine(m.Assignee, allMyIdentities)
		matchedByAlias := IsTaskMatchedByAlias(m, allMyIdentities, isDirectGmail)

		if isMarkedAsMine {
			if m.Source == "gmail" && !isDirectGmail {
				if isAssigneeGeneric(m.Assignee) {
					_ = store.UpdateTaskAssignee(email, m.ID, "")
					fixedCount++
					continue
				}
			}

			newAssignee, changed := resolveNewAssignee(user, m.Assignee, matchedByAlias)
			if changed {
				_ = store.UpdateTaskAssignee(email, m.ID, newAssignee)
				fixedCount++
			}
			continue
		}

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
		if isMeInToHeader(toHeader, user.Email) {
			if strings.TrimSpace(m.Assignee) == "" {
				_ = store.UpdateTaskAssignee(email, m.ID, getPreferredName(user))
				fixedCount++
			}
			continue
		}

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

func GetEffectiveAliases(user store.User, aliases []string) []string {
	var all []string
	if user.Name != "" {
		all = append(all, user.Name)
	}
	all = append(all, aliases...)
	return all
}

func IsTaskMatchedByAlias(m store.ConsolidatedMessage, aliases []string, isDirectGmail bool) bool {
	checkAliases := append([]string{"나", "me"}, aliases...)
	for _, a := range checkAliases {
		if a == "" {
			continue
		}
		textToCheck := m.OriginalText
		if m.Source == "gmail" && isDirectGmail {
			textToCheck = m.Task
		}
		if IsAliasMatched(textToCheck, m.Requester, a) {
			return true
		}
	}
	return false
}

func IsAliasMatched(text, requester, alias string) bool {
	if alias == "" || text == "" {
		return false
	}
	textLower := strings.ToLower(text)
	aliasLower := strings.ToLower(alias)

	if strings.Contains(textLower, "@"+aliasLower) {
		return true
	}
	if !strings.EqualFold(requester, alias) {
		if strings.Contains(textLower, aliasLower) {
			return true
		}
	}
	return false
}

func shouldClearAssignee(assignee string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	return genericOtherAssignees[norm]
}

func isAssigneeGeneric(assignee string) bool {
	norm := strings.ToLower(strings.TrimSpace(assignee))
	return norm == "" || genericMeAssignees[norm]
}

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

	limitIdx := findHeaderEnd(lowOrig, toIdx)
	toBlock := ""
	if limitIdx != -1 && limitIdx > toIdx {
		toBlock = lowOrig[toIdx:limitIdx]
	} else {
		toBlock = lowOrig[toIdx:]
	}
	return strings.Contains(toBlock, lowEmail)
}

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

func getPreferredName(user *store.User) string {
	if user.Name != "" {
		return user.Name
	}
	return user.Email
}

func extractToHeader(text string) string {
	toIdx := strings.Index(text, "To: ")
	subjIdx := strings.Index(text, ", Subject: ")
	if toIdx != -1 && subjIdx != -1 && subjIdx > toIdx {
		return text[toIdx+4 : subjIdx]
	}
	return ""
}

func isMeInToHeader(header, email string) bool {
	return header != "" && strings.Contains(strings.ToLower(header), strings.ToLower(email))
}

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

func resolveActualAssignee(ctx context.Context, m store.ConsolidatedMessage, toHeader string, svc *gmail.Service) string {
	if toHeader != "" {
		return channels.ExtractNameFromEmail(toHeader)
	}

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
