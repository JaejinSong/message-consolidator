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
	tests := []struct {
		name      string
		input     string
		expect    string
		notExpect string
	}{
		{
			name:      "Standard quote block removal",
			input:     "New message\n\nOn Wed, Mar 25, 2026 at 10:00 AM User <user@example.com> wrote:\n> Line 1\n> Line 2\n> Line 3\n> Line 4\n> Line 5\n> Line 6\n> Line 7",
			expect:    "Line 5",
			notExpect: "Line 6",
		},
		{
			name:      "Korean quote pattern",
			input:     "네 확인했습니다.\n\n2026. 3. 25. 오전 10:00 홍길동 님이 작성:\n이전 메시지 1\n이전 메시지 2\n이전 메시지 3\n이전 메시지 4\n이전 메시지 5\n이전 메시지 6",
			expect:    "이전 메시지 5",
			notExpect: "이전 메시지 6",
		},
		{
			name:      "Original Message pattern",
			input:     "Please see below.\n-----Original Message-----\nFrom: user@example.com\nTo: me@example.com\n\nOld 1\nOld 2\nOld 3\nOld 4\nOld 5\nOld 6",
			expect:    "Old 5",
			notExpect: "Old 6",
		},
		{
			name:      "Signature removal",
			input:     "This is the actual message.\n-- \nJohn Doe\nSoftware Engineer\nCompany Inc.",
			expect:    "This is the actual message.",
			notExpect: "John Doe",
		},
		{
			name:      "Signature with missing trailing space",
			input:     "Hey there,\nThanks!\n--\nJane Doe\nMarketing",
			expect:    "Thanks!",
			notExpect: "Jane Doe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanEmailBody(tt.input)
			if tt.expect != "" && !strings.Contains(got, tt.expect) {
				t.Errorf("cleanEmailBody() = %q; expected it to contain %q", got, tt.expect)
			}
			if tt.notExpect != "" && strings.Contains(got, tt.notExpect) {
				t.Errorf("cleanEmailBody() = %q; expected it NOT to contain %q", got, tt.notExpect)
			}
		})
	}
}

func TestExtractNameFromEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"Jaejin Song <jaejin@example.com>", "Jaejin Song"},
		{"<jaejin@example.com>", "jaejin@example.com"}, // Fallback to address if name is missing
		{"\"Song, Jaejin\" <jaejin@example.com>", "Song, Jaejin"},
		{"=?UTF-8?B?7Iah7J6s7KeE?= <jaejin@whatap.io>", "송재진"},          // Base64 encoded UTF-8 ("송재진")
		{"=?utf-8?B?7Iah7J6s7KeE?= <jaejin@whatap.io>", "송재진"},          // Case insensitive charset
		{"=?UTF-8?Q?Jaejin_Song?= <jaejin@example.com>", "Jaejin Song"}, // Quoted-printable encoded
		{"indonesia@whatap.io", "indonesia@whatap.io"},                  // Plain email without brackets
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractNameFromEmail(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractNameFromEmail(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}
