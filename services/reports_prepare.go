package services

import (
	"context"
	"fmt"
	"message-consolidator/store"
	"sort"
	"strings"
	"time"
)

// sanitizeMessages performs batch identity resolution to eliminate N+1 overhead.
func (s *ReportsService) sanitizeMessages(ctx context.Context, email string, msgs []Log) ([]Log, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}

	idsMap := make(map[string]bool)
	for _, m := range msgs {
		idsMap[m.Requester] = true
		idsMap[m.Assignee] = true
	}
	ids := make([]string, 0, len(idsMap))
	for id := range idsMap {
		ids = append(ids, id)
	}

	contacts, ambiguous, err := store.GetContactsByIdentifiers(ctx, email, ids)
	if err != nil {
		return msgs, err
	}

	for i := range msgs {
		m := &msgs[i]
		s.applyResolution(ctx, m, &m.Requester, &m.RequesterCanonical, &m.RequesterDisplayName, &m.RequesterType, contacts, ambiguous)
		s.applyResolution(ctx, m, &m.Assignee, &m.AssigneeCanonical, &m.AssigneeDisplayName, &m.AssigneeType, contacts, ambiguous)
	}
	return msgs, nil
}

func (s *ReportsService) applyResolution(_ context.Context, m *Log, identifierField *string, canonicalField *string, displayNameField *string, typeField *string, contacts map[string]*store.ContactRecord, ambiguous map[string]bool) {
	identifier := *identifierField
	if ambiguous[identifier] {
		*identifierField = identifier + " (Ambiguous)"
		return
	}

	if c, ok := contacts[identifier]; ok {
		*identifierField = c.CanonicalID
		*canonicalField = c.CanonicalID
		*displayNameField = c.DisplayName

		if c.ContactType != "" && c.ContactType != "none" {
			*typeField = c.ContactType
		} else if strings.HasSuffix(strings.ToLower(c.CanonicalID), "@whatap.io") || strings.EqualFold(c.CanonicalID, m.UserEmail) {
			*typeField = store.CategoryInternal
		}
	}
}

// PrepareLogsForAI formats activity and stalled logs into two labelled sections for AI input.
// Activity fills the cutoff budget first; stalled is appended with remaining budget.
func (s *ReportsService) PrepareLogsForAI(email string, activity, stalled []Log) (string, bool) {
	s.sortLogs(activity)
	s.sortLogs(stalled)
	var sb strings.Builder
	curr, truncated := 0, false
	limit := s.config.CutoffSize
	if limit <= 0 {
		limit = DefaultReportCutoffSize
	}

	statsHeader := buildActivityStatsHeader(activity)
	sb.WriteString(statsHeader)
	curr += len(statsHeader)

	activityHeader := "[Activity Tasks]\n"
	sb.WriteString(activityHeader)
	curr += len(activityHeader)

	for _, m := range activity {
		line := s.formatLogLine(email, m)
		if curr+len(line) > limit {
			truncated = true
			break
		}
		sb.WriteString(line)
		curr += len(line)
	}

	stalledHeader := "[Stalled Tasks - active items predating window]\n"
	if !truncated {
		if curr+len(stalledHeader) <= limit {
			sb.WriteString(stalledHeader)
			curr += len(stalledHeader)
			for _, m := range stalled {
				line := s.formatLogLine(email, m)
				if curr+len(line) > limit {
					truncated = true
					break
				}
				sb.WriteString(line)
				curr += len(line)
			}
		} else {
			truncated = true
		}
	}

	return sb.String(), truncated
}

// buildActivityStatsHeader pre-aggregates task counts and top open-task assignees so the model
// can skip that counting work during thinking.
func buildActivityStatsHeader(activity []Log) string {
	done, active := 0, 0
	openCounts := make(map[string]int, len(activity))
	for _, m := range activity {
		if m.Done {
			done++
			continue
		}
		active++
		key := m.AssigneeCanonical
		if key == "" {
			key = m.Assignee
		}
		openCounts[key]++
	}
	type pair struct {
		name string
		n    int
	}
	top := make([]pair, 0, len(openCounts))
	for k, v := range openCounts {
		top = append(top, pair{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n })
	if len(top) > 3 {
		top = top[:3]
	}
	parts := make([]string, len(top))
	for i, p := range top {
		parts[i] = fmt.Sprintf("%s×%d", p.name, p.n)
	}
	owners := strings.Join(parts, ", ")
	if owners == "" {
		owners = "none"
	}
	return fmt.Sprintf("# Stats: %d tasks (%d active, %d done) | Top open assignees: %s\n",
		done+active, active, done, owners)
}

func (s *ReportsService) sortLogs(logs []Log) {
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].Done != logs[j].Done {
			return !logs[i].Done
		}
		return logs[i].CreatedAt.After(logs[j].CreatedAt)
	})
}

func (s *ReportsService) formatLogLine(email string, m Log) string {
	status := " "
	if m.Done {
		status = "V"
	}

	reqName := m.RequesterDisplayName
	if reqName == "" {
		reqName = stripParenSuffix(m.Requester)
	}
	reqCat := s.resolveCategory(email, m.RequesterCanonical, m.RequesterType)
	asgName := m.AssigneeDisplayName
	if asgName == "" {
		asgName = stripParenSuffix(m.Assignee)
	}
	asgCat := s.resolveCategory(email, m.AssigneeCanonical, m.AssigneeType)

	cat := m.Category
	if cat == "" {
		cat = "TASK"
	}
	// Why: done tasks are excluded from all evidence-requiring output rules (Type A: active [ ] only;
	// Type B/C: counts and titles; Activity Rule 4: evidence not required for counting).
	// Omitting evidence entirely saves ~73 bytes per done task (~30% of input budget at 6.7x done:active ratio).
	evLen := 0
	if !m.Done {
		evLen = 180
	}
	evidence := ""
	if evLen > 0 {
		evidence = truncateEvidence(m.OriginalText, evLen)
	}

	deadlineStr := ""
	if m.Deadline != "" {
		deadlineStr = ", Due: " + m.Deadline
	}

	// Why: Age is the deterministic signal for the Stalled Tasks rule (working-day cutoff).
	// Done tasks aren't candidates and stays out of the prompt to avoid steering Activity counting.
	ageStr := formatAge(m)

	return fmt.Sprintf("- [%s][%s] %s (Room: %s, From: %s (%s), To: %s (%s)%s%s)%s\n",
		status, cat, m.Task, m.Room, reqName, reqCat, asgName, asgCat, deadlineStr, ageStr, evidence)
}

func formatAge(m Log) string {
	if m.Done {
		return ""
	}
	base := m.CreatedAt
	if !m.AssignedAt.IsZero() && m.AssignedAt.After(base) {
		base = m.AssignedAt
	}
	if base.IsZero() {
		return ""
	}
	days := store.WorkingDaysSince(base, time.Now())
	if days <= 0 {
		return ""
	}
	return fmt.Sprintf(", Age: %dwd", days)
}

// truncateEvidence extracts the newest block from OriginalText (first block post-flip)
// and returns it as a bounded " | Evidence: ..." suffix. Empty if no content.
func truncateEvidence(text string, max int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "\n\n"); idx != -1 {
		text = text[:idx]
	}
	text = strings.ReplaceAll(text, "\n", " ")
	runes := []rune(text)
	if len(runes) > max {
		runes = runes[:max]
	}
	return " | Evidence: " + string(runes)
}

func (s *ReportsService) resolveCategory(tenantEmail, canonicalID, contactType string) string {
	return store.MapContactType(contactType, strings.ToLower(canonicalID), tenantEmail)
}
