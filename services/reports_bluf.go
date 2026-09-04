package services

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"message-consolidator/store"
)

// BLUF candidate weights. Neglect signals outrank topic salience on purpose: the report
// already surfaces what was busy this window, so the BLUF's job is what the reader did not
// act on. Weights are spread far enough apart that no single axis can silently dominate.
const (
	blufWeightPinned      = 43 // explicit user flag beats every inferred signal
	blufWeightOverdue     = 37
	blufWeightResurfaced  = 31 // went quiet, then someone raised it again
	blufWeightDueSoon     = 23
	blufWeightSilent      = 19 // never touched since creation
	blufWeightVacantOwner = 17
	blufWeightMention     = 13
	blufWeightSelfOwned   = 13
	blufWeightCrossRoom   = 11
	blufWeightExternalReq = 11
	blufWeightRisk        = 11
	blufWeightAgePerDay   = 2
	blufWeightGapPerDay   = 1
)

const (
	blufMentionCap     = 5
	blufAgeCapDays     = 30
	blufGapCapDays     = 30
	blufSilentMinDays  = 7
	blufResurfaceMinD  = 7
	blufDueSoonDays    = 3
	blufCrossRoomMin   = 2
	blufMinTokenLen    = 3
	blufDossierCount   = 12
	blufHintCount      = 5
	blufHintTaskRunes  = 64
	blufEvidenceRunes  = 600
	blufDeadlineLayout = "2006-01-02"
)

// blufNoiseTokens are capitalized words that name a channel, our own product, or a calendar
// artifact rather than a subject. Complements roomNoiseTokens, which strips room scaffolding.
var blufNoiseTokens = map[string]bool{
	"gmail": true, "slack": true, "telegram": true, "whatsapp": true, "notion": true,
	"inbox": true, "sent": true, "drafts": true, "apm": true, "crm": true, "hq": true,
	// Why: our own product surfaces get capitalized in task titles ("Browser Monitoring",
	// "Collector server") and would otherwise outrank the actual counterparty on frequency.
	"browser": true, "server": true, "servers": true, "database": true, "monitoring": true,
	"agent": true, "agents": true, "dashboard": true, "alert": true, "alerts": true,
	"collector": true, "profiler": true, "license": true, "licenses": true,
	"mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true,
	"jan": true, "feb": true, "mar": true, "apr": true, "may": true, "jun": true,
	"jul": true, "aug": true, "sep": true, "oct": true, "nov": true, "dec": true,
}

// blufTopic tracks how widely one subject token appears across the scored window.
type blufTopic struct {
	mentions int
	rooms    map[string]struct{}
}

// blufDossier is one BLUF candidate with the reasons it scored, ready to hand to the model.
type blufDossier struct {
	log      Log
	score    int
	topic    string
	mentions int
	rooms    int
	signals  []string
}

// blufTopicIndex counts, per subject token, how many tasks mention it and how many distinct
// rooms it spans. Done tasks are indexed too: a topic the reader closed items on all window
// is still the window's dominant subject, even when the open item on it is a single line.
func blufTopicIndex(logs []Log) map[string]*blufTopic {
	idx := make(map[string]*blufTopic)
	for _, m := range logs {
		for _, t := range blufTopicTokens(m) {
			e := idx[t]
			if e == nil {
				e = &blufTopic{rooms: make(map[string]struct{})}
				idx[t] = e
			}
			e.mentions++
			if m.Room != "" {
				e.rooms[m.Room] = struct{}{}
			}
		}
	}
	return idx
}

// blufTopicTokens returns a task's subject tokens: the capitalized words of the title plus the
// counterparty words left in its room name. Why: capitalization is the only signal in a task
// title separating a subject ("PDRM", "Adira") from the generic verbs every task shares --
// raw word frequency ranks "review" and "report" as any window's top topics.
func blufTopicTokens(m Log) []string {
	words := blufTitleEntities(m.Task)
	if !isGenericRoom(m.Room) {
		words = append(words, blufSplitWords(stripRoomNoise(m.Room))...)
	}
	seen := make(map[string]bool, len(words))
	out := make([]string, 0, len(words))
	for _, w := range words {
		t := strings.ToLower(w)
		if len([]rune(t)) < blufMinTokenLen || seen[t] || roomNoiseTokens[t] || blufNoiseTokens[t] || isAllDigits(t) {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// blufTitleEntities returns the capitalized words of a task title, skipping the leading word
// that sentence case capitalizes regardless of meaning.
func blufTitleEntities(task string) []string {
	words := blufSplitWords(task)
	out := make([]string, 0, len(words))
	for i, w := range words {
		if i > 0 && unicode.IsUpper([]rune(w)[0]) {
			out = append(out, w)
		}
	}
	return out
}

func blufSplitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// blufDominantTopic returns the task's most-mentioned subject token with that token's mention
// count and room spread. An empty label means the task shares no subject with the window.
func blufDominantTopic(m Log, idx map[string]*blufTopic) (string, int, int) {
	best, bestN, bestRooms := "", 0, 0
	for _, t := range blufTopicTokens(m) {
		e := idx[t]
		if e == nil {
			continue
		}
		// Why: equal counts fall back to the lexically smaller token so the label does not
		// depend on title word order, keeping the rendered dossier stable across runs.
		if e.mentions > bestN || (e.mentions == bestN && t < best) {
			best, bestN, bestRooms = t, e.mentions, len(e.rooms)
		}
	}
	return best, bestN, bestRooms
}

// blufTopicScore rates how central the task's subject is to the window.
func blufTopicScore(mentions, rooms int) (int, []string) {
	capped := mentions
	if capped > blufMentionCap {
		capped = blufMentionCap
	}
	pts := capped * blufWeightMention
	labels := []string{}
	if mentions > 1 {
		labels = append(labels, fmt.Sprintf("topic mentioned %dx", mentions))
	}
	if rooms >= blufCrossRoomMin {
		pts += blufWeightCrossRoom
		labels = append(labels, fmt.Sprintf("spans %d rooms", rooms))
	}
	return pts, labels
}

// blufNeglectScore rates how long the task has gone unattended. Resurfacing outweighs plain
// age: a task that sat quiet and was then raised again is one the reader demonstrably missed,
// because someone else had to bring it back.
func blufNeglectScore(m Log, now time.Time) (int, []string) {
	pts, labels := 0, []string{}
	age := stalledAge(m, now)
	if age > 0 {
		capped := age
		if capped > blufAgeCapDays {
			capped = blufAgeCapDays
		}
		pts += capped * blufWeightAgePerDay
		labels = append(labels, fmt.Sprintf("open %d working days", age))
	}
	if m.UpdatedAt == nil {
		if calendarDays(m.CreatedAt, now) >= blufSilentMinDays {
			pts += blufWeightSilent
			labels = append(labels, fmt.Sprintf("never touched since created %d calendar days ago", calendarDays(m.CreatedAt, now)))
		}
		return pts, labels
	}
	if gap := calendarDays(m.CreatedAt, *m.UpdatedAt); gap >= blufResurfaceMinD {
		capped := gap
		if capped > blufGapCapDays {
			capped = blufGapCapDays
		}
		pts += blufWeightResurfaced + capped*blufWeightGapPerDay
		labels = append(labels, fmt.Sprintf("resurfaced after %d calendar days quiet", gap))
	}
	return pts, labels
}

// blufStakesScore rates what is at risk if the task keeps slipping.
func blufStakesScore(m Log, hostEmail string, now time.Time) (int, []string) {
	pts, labels := 0, []string{}
	if m.Pinned {
		pts += blufWeightPinned
		labels = append(labels, "pinned by user")
	}
	if p, l := blufDeadlineScore(m, now); p > 0 {
		pts += p
		labels = append(labels, l)
	}
	switch {
	case isVacantOwner(m):
		pts += blufWeightVacantOwner
		labels = append(labels, "no named owner")
	case isHostUser(m.AssigneeCanonical, m.Assignee, hostEmail):
		pts += blufWeightSelfOwned
		labels = append(labels, "owned by you")
	}
	if strings.EqualFold(m.RequesterType, "external") {
		pts += blufWeightExternalReq
		labels = append(labels, "external requester waiting")
	}
	if hasRiskKeyword(m.OriginalText) {
		pts += blufWeightRisk
		labels = append(labels, "risk wording in evidence")
	}
	return pts, labels
}

// blufDeadlineScore rates a due date's pull. An unparseable value still counts as a stated
// commitment, just without proximity credit.
func blufDeadlineScore(m Log, now time.Time) (int, string) {
	raw := strings.TrimSpace(m.DeadlineDate)
	if raw == "" {
		raw = strings.TrimSpace(m.Deadline)
	}
	if raw == "" {
		return 0, ""
	}
	d, ok := parseBLUFDeadline(raw, now.Location())
	if !ok {
		return blufWeightDueSoon, "due " + raw + " (unparsed)"
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case d.Before(today):
		return blufWeightOverdue, fmt.Sprintf("OVERDUE by %d calendar days (due %s)", calendarDays(d, today), d.Format(blufDeadlineLayout))
	case calendarDays(today, d) <= blufDueSoonDays:
		return blufWeightDueSoon, "due " + d.Format(blufDeadlineLayout)
	default:
		return 0, "due " + d.Format(blufDeadlineLayout)
	}
}

// parseBLUFDeadline accepts the RFC3339 form emitted by the report query and the bare date
// form stored on the message.
func parseBLUFDeadline(raw string, loc *time.Location) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, blufDeadlineLayout} {
		if d, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc), true
		}
	}
	return time.Time{}, false
}

func calendarDays(from, to time.Time) int {
	d := int(to.Sub(from).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// isVacantOwner reports whether the task landed on a group rather than a person, which is how
// an item ends up with everyone assuming someone else has it.
func isVacantOwner(m Log) bool {
	name := strings.ToLower(strings.TrimSpace(m.AssigneeDisplayName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(stripParenSuffix(m.Assignee)))
	}
	return name == "" || name == "shared" || name == "unassigned" || name == "unknown"
}

// isHostUser reports whether the identifier belongs to the report's own reader. Why: the
// canonical id is an email once identity resolution ran, but falls back to the raw display
// name when it did not, so both are compared.
func isHostUser(canonical, raw, hostEmail string) bool {
	if hostEmail == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(canonical), hostEmail) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(raw), hostEmail)
}

// buildBLUFDossiers ranks every open task the reader still owes -- both inside the window and
// in the pre-window backlog -- and returns the top candidates with the reasons each scored.
// Done tasks feed the topic index but are never candidates: a closed item cannot carry a BLUF,
// which by definition states who must do what by when. Tasks the user parked out of tracking
// are dropped entirely, since surfacing those would contradict an explicit decision.
func buildBLUFDossiers(activity, stalled []Log, hostEmail string, now time.Time) []blufDossier {
	all := dedupLogsByID(activity, stalled)
	idx := blufTopicIndex(all)
	out := make([]blufDossier, 0, len(all))
	for _, m := range all {
		if m.Done || m.ExcludedAt != nil {
			continue
		}
		out = append(out, scoreBLUFDossier(m, idx, hostEmail, now))
	}
	sortBLUFDossiers(out)
	if len(out) > blufDossierCount {
		out = out[:blufDossierCount]
	}
	return out
}

// dedupLogsByID concatenates the two sections, keeping the first occurrence of each task.
// Why: a task created before the window but re-touched inside it appears in BOTH activity and
// stalled -- and that overlap is exactly the resurfaced set, so it must not be double-counted
// in the topic index.
func dedupLogsByID(activity, stalled []Log) []Log {
	out := make([]Log, 0, len(activity)+len(stalled))
	seen := make(map[store.MessageID]bool, len(activity)+len(stalled))
	for _, group := range [][]Log{activity, stalled} {
		for _, m := range group {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, m)
		}
	}
	return out
}

func scoreBLUFDossier(m Log, idx map[string]*blufTopic, hostEmail string, now time.Time) blufDossier {
	topic, mentions, rooms := blufDominantTopic(m, idx)
	d := blufDossier{log: m, topic: topic, mentions: mentions, rooms: rooms}
	for _, score := range []func() (int, []string){
		func() (int, []string) { return blufTopicScore(mentions, rooms) },
		func() (int, []string) { return blufNeglectScore(m, now) },
		func() (int, []string) { return blufStakesScore(m, hostEmail, now) },
	} {
		pts, labels := score()
		d.score += pts
		d.signals = append(d.signals, labels...)
	}
	return d
}

// sortBLUFDossiers orders by score, then oldest first. Why: the recency tie-break used for
// the activity list is what collapsed the BLUF onto the newest line; among candidates that
// scored equally on neglect, the older one is the one that has been missed longer.
func sortBLUFDossiers(d []blufDossier) {
	sort.Slice(d, func(i, j int) bool {
		if d[i].score != d[j].score {
			return d[i].score > d[j].score
		}
		if !d[i].log.CreatedAt.Equal(d[j].log.CreatedAt) {
			return d[i].log.CreatedAt.Before(d[j].log.CreatedAt)
		}
		return d[i].log.ID < d[j].log.ID
	})
}

// renderBLUFDossiers formats the ranked candidates as the BLUF stage's input. Evidence runs
// far longer here than in the report payload: this stage scores a dozen tasks rather than
// counting hundreds, so the budget is better spent on why each one matters.
func renderBLUFDossiers(dossiers []blufDossier, hostEmail string) string {
	if len(dossiers) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[BLUF Candidates]\n")
	for i, d := range dossiers {
		m := d.log
		fmt.Fprintf(&sb, "[C%d] score=%d id=%d\n", i+1, d.score, m.ID)
		fmt.Fprintf(&sb, "  Task: %s\n", m.Task)
		fmt.Fprintf(&sb, "  Where: %s / %s | Topic: %s\n", m.Source, m.Room, blufTopicLabel(d.topic))
		fmt.Fprintf(&sb, "  Who: %s -> %s\n", blufParty(m.RequesterDisplayName, m.Requester, m.RequesterCanonical, m.RequesterType, hostEmail), blufParty(m.AssigneeDisplayName, m.Assignee, m.AssigneeCanonical, m.AssigneeType, hostEmail))
		fmt.Fprintf(&sb, "  Signals: %s\n", blufSignalText(d.signals))
		if ev := blufEvidenceText(m.OriginalText); ev != "" {
			fmt.Fprintf(&sb, "  Evidence: %q\n", ev)
		}
	}
	return sb.String()
}

// blufEvidenceText reuses the report payload's newest-block extraction but strips its
// " | Evidence: " infix, which only makes sense inline on a one-line task row.
func blufEvidenceText(text string) string {
	return strings.TrimPrefix(truncateEvidence(text, blufEvidenceRunes), " | Evidence: ")
}

func blufSignalText(signals []string) string {
	if len(signals) == 0 {
		return "none beyond being open"
	}
	return strings.Join(signals, "; ")
}

func blufTopicLabel(token string) string {
	if token == "" {
		return "no shared subject"
	}
	r := []rune(token)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func blufParty(display, raw, canonical, contactType, hostEmail string) string {
	name := display
	if name == "" {
		name = stripParenSuffix(raw)
	}
	// Why: identity resolution leaves canonical empty for unresolved parties, and passing that
	// empty id makes every such party read as External -- including the reader themselves.
	id := canonical
	if id == "" {
		id = stripParenSuffix(raw)
	}
	cat := store.MapContactType(contactType, strings.ToLower(id), hostEmail)
	if isHostUser(canonical, raw, hostEmail) {
		return fmt.Sprintf("%s (%s, you)", name, cat)
	}
	return fmt.Sprintf("%s (%s)", name, cat)
}

// buildBLUFCandidateLine renders the ranked shortlist as one compact Stats-block line. It is
// the fallback path: when the dedicated BLUF stage is unavailable the report prompt still gets
// a deterministic shortlist instead of falling back to its own position bias.
func buildBLUFCandidateLine(activity, stalled []Log, hostEmail string, now time.Time) string {
	dossiers := buildBLUFDossiers(activity, stalled, hostEmail, now)
	if len(dossiers) > blufHintCount {
		dossiers = dossiers[:blufHintCount]
	}
	if len(dossiers) == 0 {
		return ""
	}
	parts := make([]string, len(dossiers))
	for i, d := range dossiers {
		parts[i] = fmt.Sprintf("%d) %q [%s]", i+1, truncateRunes(d.log.Task, blufHintTaskRunes), blufSignalText(d.signals))
	}
	return "# BLUF candidates (ranked): " + strings.Join(parts, " | ") + "\n"
}
