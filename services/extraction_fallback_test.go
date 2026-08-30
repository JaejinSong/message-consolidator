package services

import (
	"message-consolidator/store"
	"strings"
	"testing"
)

func TestEnvelopeFallbackItem_ExplicitMention(t *testing.T) {
	user := store.User{Name: "Jaejin", Email: "jj@example.com"}
	p := TaskBuildParams{
		User:      user,
		SenderRaw: "Alice",
		SourceTS:  "wa1",
		OriginalText: "line one\n" + strings.Repeat("x", 100) +
			"\nplease help @Jaejin",
		ExplicitMentions: []string{"Jaejin"},
	}

	item, ok := EnvelopeFallbackItem(p)
	if !ok {
		t.Fatalf("expected fallback item to be built for explicit mention")
	}
	if item.State != "new" {
		t.Errorf("State = %q, want %q", item.State, "new")
	}
	if item.Requester != "Alice" {
		t.Errorf("Requester = %q, want %q", item.Requester, "Alice")
	}
	if item.Assignee != "Jaejin" {
		t.Errorf("Assignee = %q, want %q", item.Assignee, "Jaejin")
	}
	if item.Category != "TASK" {
		t.Errorf("Category = %q, want %q", item.Category, "TASK")
	}
	if item.SourceTS != "wa1" {
		t.Errorf("SourceTS = %q, want %q", item.SourceTS, "wa1")
	}
	if item.AssigneeReason != "envelope fallback: explicit mention while AI unavailable" {
		t.Errorf("unexpected AssigneeReason = %q", item.AssigneeReason)
	}
	if strings.Contains(item.Task, "\n") {
		t.Errorf("Task must be single-line, got %q", item.Task)
	}
	if !strings.HasSuffix(item.Task, "...") {
		t.Errorf("expected truncated Task to end with '...', got %q", item.Task)
	}
	if runes := []rune(item.Task); len(runes) != 83 { // 80 + "..."
		t.Errorf("Task length = %d, want 83", len(runes))
	}
}

func TestEnvelopeFallbackItem_NoMention(t *testing.T) {
	user := store.User{Name: "Jaejin", Email: "jj@example.com"}
	p := TaskBuildParams{
		User:         user,
		SenderRaw:    "Alice",
		SourceTS:     "wa2",
		OriginalText: "just chatting about the weather today",
	}

	if _, ok := EnvelopeFallbackItem(p); ok {
		t.Errorf("expected no fallback item without an explicit mention of the current user")
	}
}

func TestEnvelopeFallbackItem_EmptyOriginalText(t *testing.T) {
	user := store.User{Name: "Jaejin", Email: "jj@example.com"}
	p := TaskBuildParams{
		User:             user,
		SenderRaw:        "Alice",
		SourceTS:         "wa3",
		OriginalText:     "   ",
		ExplicitMentions: []string{"Jaejin"},
	}

	if _, ok := EnvelopeFallbackItem(p); ok {
		t.Errorf("expected no fallback item for empty original text")
	}
}

func TestEnvelopeFallbackItem_RequesterFallsBackToSenderEmail(t *testing.T) {
	user := store.User{Name: "Jaejin", Email: "jj@example.com"}
	p := TaskBuildParams{
		User:             user,
		SenderEmail:      "alice@example.com",
		SourceTS:         "wa4",
		OriginalText:     "please review Jaejin",
		ExplicitMentions: []string{"Jaejin"},
	}

	item, ok := EnvelopeFallbackItem(p)
	if !ok {
		t.Fatalf("expected fallback item to be built")
	}
	if item.Requester != "alice@example.com" {
		t.Errorf("Requester = %q, want SenderEmail fallback %q", item.Requester, "alice@example.com")
	}
}
