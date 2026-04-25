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
	Timestamp      interface{ IsZero() bool } // time.Time compatible
	OriginalText   string
	SourceChannels []string

	// Gmail-specific classification hint (CategorySent / CategoryMine / CategoryOthers)
	GmailClassification string
}

// BuildTask creates a ConsolidatedMessage applying shared identity rules across all channels.
//
// Rule hierarchy for Requester:
//  1. AI-extracted requester (if non-empty)
//  2. SenderRaw (resolved display name from channel metadata)
//  3. SenderEmail (raw identifier as last resort)
//
// Rule hierarchy for Assignee:
//  1. AI-extracted assignee, normalized via NormalizeAssignee
//  2. Falls back to AssigneeShared when AI returns empty
func BuildTask(p TaskBuildParams) store.ConsolidatedMessage {
	requester := resolveRequester(p)
	assignee := resolveAssignee(p)
	category := resolveCategory(p.Item.Category, p.GmailClassification)

	return store.ConsolidatedMessage{
		UserEmail:           p.UserEmail,
		Source:              p.Source,
		Room:                p.Room,
		Task:                resolveTaskTitle(p.Item.Task, p.Room, p.OriginalText),
		Requester:           requester,
		RequesterCanonical:  p.Item.RequesterCanonical,
		Assignee:            assignee,
		Link:                p.Link,
		SourceTS:            p.SourceTS,
		OriginalText:        p.OriginalText,
		Deadline:            p.Item.Deadline,
		Category:            category,
		ThreadID:            p.ThreadID,
		SourceChannels:      p.SourceChannels,
		ConsolidatedContext: p.Item.ContextSnippets,
	}
}

// resolveTaskTitle returns a non-empty, descriptive title for a task.
// Why: Empty/stub titles ("", "NONE", or <5 chars) get hidden from the dashboard
// active list (filter `IFNULL(task,'') != ''`) and report Activity counting.
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
// Prioritizes AI extraction, then raw channel metadata for zero-empty guarantee.
func resolveRequester(p TaskBuildParams) string {
	normalize := func(raw string) string {
		// Why: NormalizeContactName hits the DB; skip if no connection (e.g. unit-test without DB).
		if store.GetDB() == nil || raw == "" {
			return raw
		}
		if n := store.NormalizeContactName(p.UserEmail, raw); n != "" {
			return n
		}
		return raw
	}

	// 1. Trust AI if it provided a non-empty, non-garbage value.
	if trimmed := strings.TrimSpace(p.Item.Requester); trimmed != "" &&
		!strings.EqualFold(trimmed, "unknown") && !strings.EqualFold(trimmed, "undefined") {
		return normalize(trimmed)
	}

	// 2. Use resolved display name from channel metadata (e.g. Slack username, Gmail From header).
	if p.SenderRaw != "" {
		return normalize(p.SenderRaw)
	}

	// 3. Last resort: raw email / ID.
	return normalize(p.SenderEmail)
}


// resolveAssignee applies the Assignee normalization chain.
// Maps self-referential tokens ("me", "__CURRENT_USER__") to the user's canonical name,
// and falls back to AssigneeShared when AI returns empty.
func resolveAssignee(p TaskBuildParams) string {
	raw := strings.TrimSpace(p.Item.Assignee)
	lower := strings.ToLower(raw)

	// Explicitly bad values returned by AI → treat as empty.
	if lower == "undefined" || lower == "unknown" {
		raw = ""
	}

	if raw == "" {
		// Why: An empty assignee in Slack/WhatsApp is most likely a group/broadcast message.
		return AssigneeShared
	}

	selfTokens := map[string]bool{
		store.AssigneeMe: true, store.AssigneeCurrentUser: true,
		strings.ToLower(p.User.Name):  true,
		strings.ToLower(p.User.Email): true,
	}
	if selfTokens[lower] {
		return preferredName(p.User)
	}

	for _, alias := range p.Aliases {
		if alias != "" && strings.EqualFold(raw, alias) {
			return preferredName(p.User)
		}
	}

	if store.GetDB() != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		idType := store.ContactTypeName
		if strings.Contains(lower, "@") {
			idType = store.ContactTypeEmail
		}
		rawContactID, err1 := store.ResolveAlias(ctx, idType, lower)
		userContactID, err2 := store.ResolveAlias(ctx, store.ContactTypeEmail, strings.ToLower(p.UserEmail))
		if err1 == nil && err2 == nil && rawContactID == userContactID {
			return preferredName(p.User)
		}
	}

	return raw
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
