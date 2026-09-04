package services

import (
	"context"
	"fmt"
	"message-consolidator/store"
	"message-consolidator/types"
	"sort"
	"strings"
	"time"
	"unicode"
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
	sortStalledByAge(stalled)
	var sb strings.Builder
	curr, truncated := 0, false
	limit := s.config.CutoffSize
	if limit <= 0 {
		limit = DefaultReportCutoffSize
	}

	statsHeader := buildActivityStatsHeader(activity, stalled)
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

	// Why: the stalled section is appended only if the activity section left room; not
	// even fitting its header counts as truncation, same as running out mid-section.
	if !truncated {
		truncated = s.appendStalledSection(&sb, email, stalled, curr, limit)
	}

	return sb.String(), truncated
}

// appendStalledSection writes the stalled-task block into sb, stopping at the byte limit.
// Reports whether anything had to be dropped.
func (s *ReportsService) appendStalledSection(sb *strings.Builder, email string, stalled []Log, curr, limit int) bool {
	const stalledHeader = "[Stalled Tasks - active items predating window]\n"
	if curr+len(stalledHeader) > limit {
		return true
	}
	sb.WriteString(stalledHeader)
	curr += len(stalledHeader)
	for _, m := range stalled {
		line := s.formatLogLine(email, m)
		if curr+len(line) > limit {
			return true
		}
		sb.WriteString(line)
		curr += len(line)
	}
	return false
}

// buildActivityStatsHeader pre-aggregates task counts, ownership concentration, room→customer
// mapping, and cross-source signals so the model can skip that counting work during thinking.
func buildActivityStatsHeader(activity, stalled []Log) string {
	done, active, totalOpen := 0, 0, 0
	openCounts := make(map[string]int, len(activity))
	for _, m := range activity {
		if m.Done {
			done++
			continue
		}
		active++
		totalOpen++
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
	var typeBTrigger string
	for i, p := range top {
		pct := 0
		if totalOpen > 0 {
			pct = p.n * 100 / totalOpen
		}
		parts[i] = fmt.Sprintf("%s×%d(%d%%)", p.name, p.n, pct)
		if pct > 40 && typeBTrigger == "" {
			typeBTrigger = p.name
		}
	}
	owners := strings.Join(parts, ", ")
	if owners == "" {
		owners = "none"
	}
	assigneeLine := "# Top open assignees: " + owners
	if typeBTrigger != "" {
		assigneeLine += " | Type B trigger: " + typeBTrigger
	}

	roomCustomer := buildRoomCustomerMap(activity)
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Stats: %d activity (%d active, %d done) | %d stalled\n",
		done+active, active, done, len(stalled))
	sb.WriteString(assigneeLine + "\n")
	sb.WriteString(buildRoomCustomerLine(roomCustomer))
	sb.WriteString(buildCrossSourceLine(activity, roomCustomer))
	return sb.String()
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
		cat = string(types.CategoryTask)
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
		if evidence != "" && hasRiskKeyword(m.OriginalText) {
			evidence += " [RISK-CAND]"
		}
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

// sortStalledByAge sorts stalled tasks by working-day age descending (oldest first).
func sortStalledByAge(logs []Log) {
	now := time.Now()
	sort.Slice(logs, func(i, j int) bool {
		return stalledAge(logs[i], now) > stalledAge(logs[j], now)
	})
}

func stalledAge(m Log, now time.Time) int {
	base := m.CreatedAt
	if !m.AssignedAt.IsZero() && m.AssignedAt.After(base) {
		base = m.AssignedAt
	}
	return store.WorkingDaysSince(base, now)
}

var riskKeywords = []string{
	"scalab", "blocker", "block", "delay", "concern", "urgent", "risk",
	"can't", "cannot", "isn't", "unable", "stuck", "slow", "waiting", "issue",
}

func hasRiskKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, kw := range riskKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// inferCustomer derives a room's counterparty. Generic multi-customer channels bucket first
// so one task's counterparty can never label the whole channel, then a named entity in the
// task, then the room name. An empty result means the room stays unresolved.
func inferCustomer(task, room string) string {
	if isGenericRoom(room) {
		return "Other Tasks"
	}
	if c := inferCustomerFromTask(task); c != "" {
		return c
	}
	return inferCustomerFromRoom(room)
}

// entityNameMaxWords caps how long a "for X" tail may run before it reads as prose.
const entityNameMaxWords = 3

// inferCustomerFromTask extracts a counterparty using the "for X" pattern, accepting only
// candidates shaped like a proper name. Why: descriptive tails ("for cleanup/archive
// guidance", "for FIF telemetry data gap") otherwise became customer labels, and once they
// reached the Room->Customer map the report model treated them as authoritative.
func inferCustomerFromTask(task string) string {
	lower := strings.ToLower(task)
	idx := strings.Index(lower, " for ")
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(task[idx+5:])
	for i, r := range rest {
		if r == ',' || r == '.' || r == '(' || r == '\n' {
			rest = strings.TrimSpace(rest[:i])
			break
		}
	}
	if !isEntityName(rest) {
		return ""
	}
	return rest
}

// isEntityName reports whether s reads as a proper name: a few words, each starting with an
// uppercase letter or a digit (acronyms, "Bank BNI", "V4").
func isEntityName(s string) bool {
	words := strings.Fields(s)
	if len(words) == 0 || len(words) > entityNameMaxWords {
		return false
	}
	for _, w := range words {
		first := []rune(w)[0]
		if !unicode.IsUpper(first) && !unicode.IsDigit(first) {
			return false
		}
	}
	return true
}

var genericRoomPrefixes = []string{"gmail", "inbox", "sent", "drafts", "slack", "dm"}

// isGenericRoom reports whether the room is a mailbox or chat surface shared across many
// counterparties, so no single customer can be attributed to it.
func isGenericRoom(room string) bool {
	lower := strings.ToLower(room)
	for _, g := range genericRoomPrefixes {
		if lower == g || strings.HasPrefix(lower, g+"-") || strings.HasPrefix(lower, g+" ") {
			return true
		}
	}
	return false
}

// roomNoiseTokens are the vendor, project and channel-scaffolding words shared room names
// carry alongside the actual counterparty ("Adira - Whatap Tech", "PDRM POC - MSB | IFC |
// WhaTap"). Dropping them leaves the counterparty behind.
var roomNoiseTokens = map[string]bool{
	"whatap": true, "poc": true, "project": true, "internal": true, "tech": true,
	"ifc": true, "msb": true, "team": true, "group": true, "chat": true, "room": true,
	"x": true, "and": true, "the": true,
}

func inferCustomerFromRoom(room string) string {
	trimmed := strings.TrimSpace(room)
	// Why: WhatsApp @lid chats land here as a bare numeric id with no display name anywhere in
	// our data (wa_messages.chat_name falls back to the same id). The id carries no customer
	// signal, so bucket it explicitly rather than leaving the model to read meaning into it.
	if trimmed == "" || isGenericRoom(room) || isAllDigits(trimmed) {
		return "Other Tasks"
	}
	const bizGlobalPfx = "biz-global-"
	if lower := strings.ToLower(room); strings.HasPrefix(lower, bizGlobalPfx) {
		if country := room[len(bizGlobalPfx):]; country != "" {
			return titleFirst(country) + " Biz"
		}
	}
	// Why: an unchanged room name is not an inference. Emitting room->room would hand the
	// report model a channel name to use as a customer, which rule 4 forbids; empty keeps the
	// room out of the map so the model applies its own inference instead.
	if c := stripRoomNoise(room); c != "" && c != strings.TrimSpace(room) {
		return c
	}
	return ""
}

// stripRoomNoise splits a room name on its separators and drops vendor scaffolding and
// numeric channel ids, returning what is left of the counterparty.
func stripRoomNoise(room string) string {
	fields := strings.FieldsFunc(room, func(r rune) bool {
		return r == '-' || r == '|' || r == '/' || r == '_' || unicode.IsSpace(r)
	})
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if roomNoiseTokens[strings.ToLower(f)] || isAllDigits(f) {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func titleFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func buildRoomCustomerMap(activity []Log) map[string]string {
	result := make(map[string]string)
	for _, m := range activity {
		if m.Room == "" {
			continue
		}
		if _, ok := result[m.Room]; ok {
			continue
		}
		// Why: an unresolved room is left out of the map entirely, so the model applies rule
		// 4's own inference rather than trusting a mapping we could not actually derive.
		if c := inferCustomer(m.Task, m.Room); c != "" {
			result[m.Room] = c
		}
	}
	return result
}

// crossSourceMinRooms is how many distinct rooms one customer must appear in before the
// cross-source hint is worth showing.
const crossSourceMinRooms = 3

// roomsByCustomer groups the activity's rooms under their customer, skipping rooms with no
// customer mapping and the catch-all bucket.
func roomsByCustomer(activity []Log, roomCustomer map[string]string) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, m := range activity {
		c := roomCustomer[m.Room]
		if c == "" || c == "Other Tasks" {
			continue
		}
		if out[c] == nil {
			out[c] = make(map[string]struct{})
		}
		out[c][m.Room] = struct{}{}
	}
	return out
}

// firstMultiRoomCustomer picks the alphabetically first customer spanning enough rooms,
// with its rooms sorted. Why: map iteration order is random, so the tie-break keeps the
// rendered line stable across runs.
func firstMultiRoomCustomer(customerRooms map[string]map[string]struct{}) (string, []string) {
	best, bestRooms := "", []string(nil)
	for c, rooms := range customerRooms {
		if len(rooms) < crossSourceMinRooms {
			continue
		}
		if best != "" && c >= best {
			continue
		}
		list := make([]string, 0, len(rooms))
		for r := range rooms {
			list = append(list, r)
		}
		sort.Strings(list)
		best, bestRooms = c, list
	}
	return best, bestRooms
}

func buildCrossSourceLine(activity []Log, roomCustomer map[string]string) string {
	best, bestRooms := firstMultiRoomCustomer(roomsByCustomer(activity, roomCustomer))
	if best == "" {
		return ""
	}
	return fmt.Sprintf("# Cross-source: \"%s\" in [%s]\n", best, strings.Join(bestRooms, ", "))
}

func buildRoomCustomerLine(roomCustomer map[string]string) string {
	if len(roomCustomer) == 0 {
		return ""
	}
	rooms := make([]string, 0, len(roomCustomer))
	for r := range roomCustomer {
		rooms = append(rooms, r)
	}
	sort.Strings(rooms)
	parts := make([]string, len(rooms))
	for i, r := range rooms {
		parts[i] = r + "→" + roomCustomer[r]
	}
	return "# Room→Customer: " + strings.Join(parts, ", ") + "\n"
}
