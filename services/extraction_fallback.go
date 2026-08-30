package services

import (
	"strings"

	"message-consolidator/store"
	"message-consolidator/types"
)

const envelopeFallbackTaskMaxRunes = 80 // Why: keeps deterministic fallback titles bounded like AI-produced titles.

// EnvelopeFallbackItem builds a minimal deterministic TodoItem when AI extraction is unavailable.
// Returns false when the message does not explicitly involve the current user.
// Why: fires only on an explicit mention (name/email/alias/@mention) so an AI outage cannot
// drift into extracting every message in a room -- only already-agreed envelope facts.
func EnvelopeFallbackItem(p TaskBuildParams) (store.TodoItem, bool) {
	if strings.TrimSpace(p.OriginalText) == "" {
		return store.TodoItem{}, false
	}
	if !currentUserMentionedInText(p) {
		return store.TodoItem{}, false
	}

	requester := p.SenderRaw
	if requester == "" {
		requester = p.SenderEmail
	}

	return store.TodoItem{
		State:          "new",
		Task:           truncateFallbackTask(p.OriginalText),
		Requester:      requester,
		Assignee:       preferredName(p.User),
		Category:       string(types.CategoryTask),
		SourceTS:       p.SourceTS,
		AssigneeReason: "envelope fallback: explicit mention while AI unavailable",
	}, true
}

// truncateFallbackTask collapses the original text to a single line and caps it at
// envelopeFallbackTaskMaxRunes runes, appending "..." when truncated.
func truncateFallbackTask(original string) string {
	oneLine := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(original), "\r\n", " "), "\n", " ")
	runes := []rune(oneLine)
	if len(runes) <= envelopeFallbackTaskMaxRunes {
		return oneLine
	}
	return string(runes[:envelopeFallbackTaskMaxRunes]) + "..."
}
