package scanner

import (
	"fmt"
	"strings"
	"time"
)

// formatTSTag renders the "[ts:...]" prefix shared by every chat channel
// payload line, or "" when the message carries no timestamp.
func formatTSTag(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return fmt.Sprintf("[ts:%s]", ts.UTC().Format("2006-01-02T15:04"))
}

// formatTagBlock renders the " [Tags: a, b]" block shared by WhatsApp,
// Telegram and Slack payload lines, or "" when tags is empty.
func formatTagBlock(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	return fmt.Sprintf(" [Tags: %s]", strings.Join(tags, ", "))
}

// formatListBlock renders a labelled " [Label: a, b]" block (used for Files,
// Reactions, etc.), or "" when items is empty.
func formatListBlock(label string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf(" [%s: %s]", label, strings.Join(items, ", "))
}

// writePayloadLine appends one "[ID:...][ts:...]<meta> sender: text\n" line —
// the shared prompt payload format for WhatsApp, Telegram and Slack messages.
func writePayloadLine(sb *strings.Builder, id, tsTag, metaStr, sender, text string) {
	fmt.Fprintf(sb, "[ID:%s]%s%s %s: %s\n", id, tsTag, metaStr, sender, text)
}
