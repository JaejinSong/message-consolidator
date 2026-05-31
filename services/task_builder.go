package services

import (
	"context"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"time"
)

// TaskBuildParams holds cross-channel inputs required for building a consolidated message.
// Why: Single input type prevents argument sprawl and makes the factory function stable as new channels are added.
type TaskBuildParams struct {
	UserEmail string
	User      store.User
	Aliases   []string
	Item      store.TodoItem

	// Raw metadata from the source channel — used as authoritative fallback if AI leaves fields empty.
	SenderRaw   string // Resolved display name (e.g., "WhaTap Bot", "Kenny")
	SenderEmail string // Raw email/ID (e.g., "kenny@whatap.io")
	ToHeader    string // Primary recipient header (for Gmail assignee resolution)

	Source         string
	Room           string
	Link           string
	ThreadID       string
	SourceTS       string
	Timestamp      time.Time // Envelope message timestamp → ConsolidatedMessage.AssignedAt
	RepliedToID    string    // Envelope reply chain → ConsolidatedMessage.RepliedToID (Slack thread/Telegram reply/WA quoted)
	OriginalText   string
	SourceChannels []string

	// Gmail-specific classification hint (CategorySent / CategoryMine / CategoryOthers)
	GmailClassification string

	// IsCcOnly indicates the user appears only in Cc (not To/Bcc/From). Envelope-driven
	// signal that overrides AI self-assignment so informational copies route to the
	// Reference tab instead of the user's Inbox.
	IsCcOnly bool

	// Why: envelope-resolved explicit mentions in document order. Slack <@USERID> resolved
	//      to display names; WA MentionedJID resolved to push names. First-pick fallback
	//      activates when AI returns "shared" for messages that explicitly addressed people.
	ExplicitMentions []string
}

// BuildTask creates a ConsolidatedMessage applying shared identity rules across all channels.
//
// Rule hierarchy for Requester (envelope-driven; AI is fallback only):
//  1. SenderRaw (resolved display name from channel metadata)
//  2. SenderEmail (raw identifier)
//  3. AI-extracted requester (only when both envelope sources are empty)
//
// Rule hierarchy for Assignee:
//  1. AI-extracted assignee, normalized via NormalizeAssignee
//  2. Category=PROMISE with empty AI → SenderRaw (envelope-driven speaker fallback)
//  3. Falls back to AssigneeShared when AI returns empty
func BuildTask(ctx context.Context, p TaskBuildParams) store.ConsolidatedMessage {
	requester := resolveRequester(ctx, p)
	assignee := resolveAssignee(ctx, p)
	category := resolveCategory(p.Item.Category, p.GmailClassification)
	deadlineDate, deadlineInferred := ParseDeadline(p.Item.Deadline, p.Timestamp)

	return store.ConsolidatedMessage{
		UserEmail:           p.UserEmail,
		Source:              p.Source,
		Room:                p.Room,
		Task:                resolveTaskTitle(p.Item.Task, p.Room, p.OriginalText),
		Requester:           requester,
		RequesterCanonical:  p.Item.RequesterCanonical,
		Assignee:            assignee,
		AssignedAt:          p.Timestamp,
		Link:                p.Link,
		SourceTS:            p.SourceTS,
		OriginalText:        p.OriginalText,
		Deadline:            p.Item.Deadline,
		DeadlineDate:        deadlineDate,
		DeadlineInferred:    deadlineInferred,
		Category:            category,
		ThreadID:            p.ThreadID,
		RepliedToID:         p.RepliedToID,
		SourceChannels:      p.SourceChannels,
		ConsolidatedContext: p.Item.ContextSnippets,
	}
}

// resolveTaskTitle returns a non-empty, descriptive title for a task.
// Why: Empty/stub titles ("", "NONE", or <5 chars) get hidden from the dashboard
// active list (filter `IFNULL(task,”) != ”`) and report Activity counting.
// Falls back through Gmail subject line → original snippet → room marker so every
// row carries a minimum identifier even if upstream AI returns garbage.
func resolveTaskTitle(aiTitle, room, original string) string {
	if t := strings.TrimSpace(aiTitle); len(t) >= 5 && !strings.EqualFold(t, "NONE") {
		return t
	}
	for _, line := range strings.Split(original, "\n") {
		if strings.HasPrefix(line, "S: ") {
			if s := strings.TrimSpace(line[2:]); len(s) >= 5 {
				return s
			}
		}
	}
	cleaned := strings.TrimSpace(strings.ReplaceAll(original, "\n", " "))
	runes := []rune(cleaned)
	if len(runes) >= 5 {
		if len(runes) > 60 {
			runes = runes[:60]
		}
		return strings.TrimSpace(string(runes))
	}
	if room != "" {
		return "[" + room + "]"
	}
	return "[Unidentified message]"
}

// resolveRequester applies the Requester fallback chain.
// Phase J Path B: envelope (SenderRaw / SenderEmail) is authoritative; AI is last-resort fallback.
// Self-DM exception: when sender is the current user and AI emitted a distinct requester
// (chat_system 1.9.0 reported-speech rule), the AI value overrides envelope to preserve the
// original external requester recorded inside the memo body.
func resolveRequester(ctx context.Context, p TaskBuildParams) string {
	normalize := func(raw string) string {
		// Why: NormalizeContactName hits the DB; skip if no connection (e.g. unit-test without DB).
		if store.GetDB() == nil || raw == "" {
			return raw
		}
		if n := store.NormalizeContactName(ctx, p.UserEmail, raw); n != "" {
			return n
		}
		return raw
	}

	if ai := aiRequesterOverrideForSelfDM(ctx, p); ai != "" {
		return normalize(ai)
	}

	// Why (Phase J Path B): envelope is metadata-driven. SenderRaw / SenderEmail come from the platform
	// adapter and are authoritative. AI extraction is allowed only as last-resort fallback when the
	// adapter could not resolve the sender at all.
	if p.SenderRaw != "" {
		return normalize(p.SenderRaw)
	}
	if p.SenderEmail != "" {
		return normalize(p.SenderEmail)
	}

	if trimmed := strings.TrimSpace(p.Item.Requester); trimmed != "" &&
		!strings.EqualFold(trimmed, "unknown") && !strings.EqualFold(trimmed, "undefined") {
		return normalize(trimmed)
	}
	return ""
}

// aiRequesterOverrideForSelfDM returns the AI-extracted requester when the envelope sender is
// the current user and AI identified a distinct external requester (self-DM reported-speech).
// Returns "" otherwise.
func aiRequesterOverrideForSelfDM(ctx context.Context, p TaskBuildParams) string {
	ai := strings.TrimSpace(p.Item.Requester)
	if ai == "" || strings.EqualFold(ai, "unknown") || strings.EqualFold(ai, "undefined") {
		return ""
	}
	if isSelfReference(ai, p) || matchesAlias(ai, p.Aliases) {
		return ""
	}
	if !senderIsCurrentUser(ctx, p) {
		return ""
	}
	return ai
}

func senderIsCurrentUser(ctx context.Context, p TaskBuildParams) bool {
	if p.SenderRaw != "" && (isSelfReference(p.SenderRaw, p) || matchesAlias(p.SenderRaw, p.Aliases) || resolvesToCurrentUser(ctx, p.SenderRaw, p)) {
		return true
	}
	if p.SenderEmail != "" && (isSelfReference(p.SenderEmail, p) || matchesAlias(p.SenderEmail, p.Aliases) || resolvesToCurrentUser(ctx, p.SenderEmail, p)) {
		return true
	}
	return false
}

func resolveAssignee(ctx context.Context, p TaskBuildParams) string {
	raw := normalizeAIAssignee(p)
	if raw == "" {
		raw = AssigneeShared
	}
	// Why: User is only on Cc — informational copy. AI's self-assignment bias must not
	// pull the task into the user's Inbox. Falls through to AssigneeShared so assignCategory
	// resolves to CategoryShared → handler default → Reference tab.
	if p.IsCcOnly && (isSelfReference(raw, p) || matchesAlias(raw, p.Aliases) || resolvesToCurrentUser(ctx, raw, p)) {
		return AssigneeShared
	}
	// Why: Group chat self-assignment bias guard — __CURRENT_USER__ is rejected when
	// the user is not textually present in the message. Channel membership alone is
	// not addressing (biz-global-thailand Srisawad incident).
	if isGroupSource(p.Source) && (isSelfReference(raw, p) || matchesAlias(raw, p.Aliases)) {
		if !currentUserMentionedInText(p) {
			if picked := pickFirstMentionAssignee(p); picked != "" {
				return picked
			}
			return AssigneeShared
		}
	}
	if isSelfReference(raw, p) {
		return preferredName(p.User)
	}
	if matchesAlias(raw, p.Aliases) {
		return preferredName(p.User)
	}
	if resolvesToCurrentUser(ctx, raw, p) {
		return preferredName(p.User)
	}
	if raw == AssigneeShared {
		if picked := pickFirstMentionAssignee(p); picked != "" {
			return picked
		}
		if picked := pickRoomDefaultActor(ctx, p); picked != "" {
			return picked
		}
	}
	return raw
}

// Why (M): first explicit mention is the linguistic primary actor; activates only when AI
//
//	returned "shared" despite explicit recipients (e.g. multi-mention compliance gap).
func pickFirstMentionAssignee(p TaskBuildParams) string {
	for _, name := range p.ExplicitMentions {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if isSelfReference(trimmed, p) || matchesAlias(trimmed, p.Aliases) {
			return preferredName(p.User)
		}
		return trimmed
	}
	return ""
}

// Why (R): per-room dominant actor (count≥2, ≥50% of non-shared/non-requester assignments).
//
//	Activates only for shared fallback when room has a clear single-actor pattern
//	and the requester themselves isn't that actor (avoids self-assignment).
func pickRoomDefaultActor(ctx context.Context, p TaskBuildParams) string {
	if p.Room == "" || store.GetDB() == nil {
		return ""
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	actor, ok := store.GetRoomDefaultActor(dbCtx, p.UserEmail, p.Room)
	if !ok {
		return ""
	}
	requester := strings.TrimSpace(p.Item.Requester)
	if requester != "" && strings.EqualFold(requester, actor) {
		return ""
	}
	if isSelfReference(actor, p) || matchesAlias(actor, p.Aliases) {
		return preferredName(p.User)
	}
	return actor
}

func normalizeAIAssignee(p TaskBuildParams) string {
	raw := strings.TrimSpace(p.Item.Assignee)
	lower := strings.ToLower(raw)
	// Explicitly bad values returned by AI → treat as empty.
	if lower == "undefined" || lower == "unknown" {
		raw = ""
	}
	// Why: PROMISE에서 AI가 보수적으로 "shared"를 반환할 때 envelope speaker로 override.
	//      tie-break rule 영향으로 1인칭 약속도 shared로 분류되는 회귀를 차단.
	if strings.EqualFold(raw, "shared") && strings.EqualFold(p.Item.Category, "PROMISE") && p.SenderRaw != "" {
		raw = ""
	}
	// Why (Phase J Path B): chat_system Assignee rule 4 (`category=PROMISE → Sender`) moved from prompt to code so envelope drives the speaker fallback. Applies only when AI left assignee blank — explicit AI assignments still win.
	if raw == "" && strings.EqualFold(p.Item.Category, "PROMISE") && p.SenderRaw != "" {
		raw = p.SenderRaw
	}
	return raw
}

func isSelfReference(raw string, p TaskBuildParams) bool {
	lower := strings.ToLower(raw)
	return lower == store.AssigneeMe ||
		lower == store.AssigneeCurrentUser ||
		lower == strings.ToLower(p.User.Name) ||
		lower == strings.ToLower(p.User.Email)
}

func matchesAlias(raw string, aliases []string) bool {
	for _, alias := range aliases {
		if alias != "" && strings.EqualFold(raw, alias) {
			return true
		}
	}
	return false
}

// Why: ResolveAlias is the only DB-backed branch — wrapped so resolveAssignee stays in cognitive budget. Gracefully returns false if the DB hasn't been initialized.
func resolvesToCurrentUser(ctx context.Context, raw string, p TaskBuildParams) bool {
	if store.GetDB() == nil {
		return false
	}
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	idType := store.ContactTypeName
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "@") {
		idType = store.ContactTypeEmail
	}
	rawContactID, err1 := store.ResolveAlias(dbCtx, idType, lower)
	if err1 != nil {
		return false
	}
	userContactID, err2 := store.ResolveAlias(dbCtx, store.ContactTypeEmail, strings.ToLower(p.UserEmail))
	if err2 != nil {
		return false
	}
	return rawContactID == userContactID
}

// resolveCategory falls back to a sensible default when both AI category and Gmail classification are empty.
func resolveCategory(aiCategory, gmailCls string) string {
	if aiCategory != "" {
		return aiCategory
	}
	if gmailCls != "" {
		return gmailCls
	}
	return string(types.CategoryTask)
}

func preferredName(u store.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

// isGroupSource reports whether the source is a multi-participant chat platform
// where messages are not inherently directed at the current user.
func isGroupSource(source string) bool {
	s := strings.ToLower(source)
	return s == "slack" || s == "telegram" || s == "whatsapp"
}

// currentUserMentionedInText reports whether the current user is explicitly
// referenced in the message body via name, email, alias, or resolved @mention.
func currentUserMentionedInText(p TaskBuildParams) bool {
	lower := strings.ToLower(p.OriginalText)
	if p.User.Name != "" && strings.Contains(lower, strings.ToLower(p.User.Name)) {
		return true
	}
	if p.User.Email != "" && strings.Contains(lower, strings.ToLower(p.User.Email)) {
		return true
	}
	for _, a := range p.Aliases {
		if a != "" && strings.Contains(lower, strings.ToLower(a)) {
			return true
		}
	}
	for _, m := range p.ExplicitMentions {
		t := strings.TrimSpace(m)
		if t != "" && (isSelfReference(t, p) || matchesAlias(t, p.Aliases)) {
			return true
		}
	}
	return false
}
