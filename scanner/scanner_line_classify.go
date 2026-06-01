package scanner

import (
	"strings"

	"message-consolidator/db"
	"message-consolidator/store"
	"message-consolidator/types"
)

// classifyLineMessage decides relevance of a LINE inbox row for a given user.
// Returns CategoryTask when the message is clearly actionable, CategoryQuery otherwise.
//
// Strategy (mirrors scanner_slack_classify):
//  1. Group/room chat (broadcast) → always Task
//  2. Alias match in text → Task
//  3. Fallback → Query
//
// Why: LINE userId-based mention matching requires user to register their LINE ID, which
// is not yet supported. Broadcast + alias covers the common use case until then.
func classifyLineMessage(row db.LineInbox, user *store.User, aliases []string) types.MessageCategory {
	if isLineBroadcast(row) {
		return types.CategoryTask
	}
	if hasLineAliasMatch(row.Text, user, aliases) {
		return types.CategoryTask
	}
	return types.CategoryQuery
}

// isLineBroadcast returns true for group/room chats where any member may be the recipient.
func isLineBroadcast(row db.LineInbox) bool {
	return row.ChatType == "group" || row.ChatType == "room"
}

// hasLineAliasMatch reports whether any alias or user name appears in the message text.
func hasLineAliasMatch(text string, user *store.User, aliases []string) bool {
	lower := strings.ToLower(text)
	if user != nil && user.Name != "" && strings.Contains(lower, strings.ToLower(user.Name)) {
		return true
	}
	for _, a := range aliases {
		if a != "" && strings.Contains(lower, strings.ToLower(a)) {
			return true
		}
	}
	return false
}
