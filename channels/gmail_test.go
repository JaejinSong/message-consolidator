package channels

import (
	"message-consolidator/store"
	"strings"
	"testing"
)

func TestClassifyGmail(t *testing.T) {
	tests := []struct {
		isFromMe bool
		isTo     bool
		expected string
	}{
		{true, true, CategorySent},
		{true, false, CategorySent},
		{false, true, CategoryMine},
		{false, false, CategoryOthers},
	}

	for _, tt := range tests {
		if got := classifyGmail(tt.isFromMe, tt.isTo); got != tt.expected {
			t.Errorf("classifyGmail(%v, %v) = %v; want %v", tt.isFromMe, tt.isTo, got, tt.expected)
		}
	}
}

func TestCheckRecipientStatus(t *testing.T) {
	email := "me@example.com"
	from := "sender@example.com"
	to := "me@example.com, other@example.com"
	cc := "cc@example.com"
	bcc := ""
	deliveredTo := "me@example.com"

	isFromMe, isTo, isCc, _, isDelTo := checkRecipientStatus(email, from, to, cc, bcc, deliveredTo)

	if isFromMe {
		t.Errorf("Expected isFromMe to be false")
	}
	if !isTo {
		t.Errorf("Expected isTo to be true")
	}
	if isCc {
		t.Errorf("Expected isCc to be false")
	}
	if !isDelTo {
		t.Errorf("Expected isDelTo to be true")
	}

	// Case with CC only
	to2 := "other@example.com"
	cc2 := "me@example.com"
	_, isTo2, isCc2, _, _ := checkRecipientStatus(email, from, to2, cc2, bcc, deliveredTo)
	if isTo2 {
		t.Errorf("Expected isTo2 to be false")
	}
	if !isCc2 {
		t.Errorf("Expected isCc2 to be true")
	}
}

func TestResolveGmailCategoryAndAssignee(t *testing.T) {
	fallback := "Jaejin Song"
	
	// Case 1: My Task (CategoryMine)
	item1 := store.TodoItem{Assignee: "Jaejin Song", Category: "todo"}
	asg1, cat1 := resolveGmailCategoryAndAssignee(item1, true, CategoryMine, "me@example.com", fallback)
	if asg1 != fallback || cat1 != "todo" {
		t.Errorf("Case 1 (My Task): got %s, %s; want %s, todo", asg1, cat1, fallback)
	}

	// Case 2: Other Task (CategoryOthers) - CC case
	item2 := store.TodoItem{Assignee: "me", Category: "todo"}
	asg2, cat2 := resolveGmailCategoryAndAssignee(item2, true, CategoryOthers, "other@example.com <other@example.com>", fallback)
	if asg2 != "other@example.com" || cat2 != "todo" {
		t.Errorf("Case 2 (CC/Others): got %s, %s; want other@example.com, todo", asg2, cat2)
	}

	// Case 3: Other Task (CategoryOthers) - Group mail case
	item3 := store.TodoItem{Assignee: "me", Category: "todo"}
	asg3, cat3 := resolveGmailCategoryAndAssignee(item3, true, CategoryOthers, "indonesia@whatap.io", fallback)
	if asg3 != "indonesia@whatap.io" || cat3 != "todo" {
		t.Errorf("Case 3 (Group mail): got %s, %s; want indonesia@whatap.io, todo", asg3, cat3)
	}
}

func TestStripHTML(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"<div>Hello World</div>", "Hello World"},
		{"<p>Line 1</p><p>Line 2</p>", "Line 1 Line 2"},
		{"Hello&nbsp;World &amp; Co.", "Hello World & Co."},
		{"<style>.foo { color: red; }</style>Body", "Body"}, // Style tags are now correctly removed
	}

	for _, tt := range tests {
		if got := stripHTML(tt.input); got != tt.expected {
			t.Errorf("stripHTML(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCleanEmailBody(t *testing.T) {
	body := `New message

On Wed, Mar 25, 2026 at 10:00 AM User <user@example.com> wrote:
> Line 1
> Line 2
> Line 3
> Line 4
> Line 5
> Line 6
> Line 7`

	cleaned := cleanEmailBody(body)
	lines := strings.Split(cleaned, "\n")
	
	count := 0
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), ">") {
			count++
		}
	}

	if count > 5 {
		t.Errorf("Expected at most 5 quoted lines, got %d", count)
	}
	
	if !strings.Contains(cleaned, "Line 4") {
		t.Errorf("Expected context 'Line 4' to be preserved")
	}
	
	if strings.Contains(cleaned, "Line 6") {
		t.Errorf("Expected 'Line 6' to be truncated")
	}
}
