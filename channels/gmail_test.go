package channels

import (
	"context"
	"message-consolidator/config"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"testing"
	"google.golang.org/api/gmail/v1"
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

func TestBuildTask_GmailIdentityFallback(t *testing.T) {
	// Why: Proves that the Unified Builder fills in Requester from SenderRaw when AI returns empty.
	user := store.User{Name: "Jaejin Song", Email: "jaejin@example.com"}
	item := store.TodoItem{Task: "Check server", Requester: "", Assignee: "__CURRENT_USER__"}

	params := services.TaskBuildParams{
		UserEmail: user.Email, User: user, Item: item,
		SenderRaw: "Kenny Park", Source: "gmail", Room: "Gmail",
		GmailClassification: CategoryMine,
	}
	msg := services.BuildTask(params)

	if msg.Requester != "Kenny Park" {
		t.Errorf("Expected Requester='Kenny Park', got %q", msg.Requester)
	}
	if msg.Assignee != "Jaejin Song" {
		t.Errorf("Expected Assignee='Jaejin Song' from __CURRENT_USER__, got %q", msg.Assignee)
	}
}


func TestStripHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Simple div", "<div>Hello World</div>", "Hello World"},
		{"Paragraphs", "<p>Line 1</p><p>Line 2</p>", "Line 1 Line 2"},
		{"Entities", "Hello&nbsp;World &amp; Co.", "Hello World & Co."},
		{"Multi-line Style", "<style>\n.foo { color: red; }\nbody { background: #fff; }\n</style>Body Content", "Body Content"},
		{"Script removal", "<script type=\"text/javascript\">\nalert('hello');\n</script>Visible Text", "Visible Text"},
		{"Comments", "Visible <!-- hidden comment --> Text", "Visible Text"},
		{"Nested elements", "<div>Outer <p>Inner</p></div>", "Outer Inner"},
		{"Table structures", "<table><tr><td>Cell 1</td><td>Cell 2</td></tr></table>", "Cell 1 Cell 2"},
		{"Complex Mix", "<html><head><style>body{}</style></head><body><h1>Title</h1><p>Para <a href='http://ext.com'>Link</a></p></body></html>", "Title Para Link"},
		{"Gmail Quote Pruning", "<div>Hello<div class='gmail_quote'><div class='gmail_attr'>Sender wrote:</div><blockquote class='gmail_quote'>Quote</blockquote></div>World</div>", "Hello World"},
		{"Blockquote Pruning", "<div>Main Text<blockquote>Quoted Text</blockquote>Footer</div>", "Main Text Footer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripHTML(tt.input)
			if got != tt.expected {
				t.Errorf("stripHTML() = %q; want %q", got, tt.expected)
			}
		})
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
			name:      "Signature removal",
			input:     "This is the actual message.\n\n-- \nJohn Doe\nSoftware Engineer\nCompany Inc.",
			expect:    "This is the actual message.",
			notExpect: "John Doe",
		},
		{
			name:      "Signature with missing trailing space",
			input:     "Hey there,\nThanks!\n\n--\nJane Doe\nMarketing",
			expect:    "Thanks!",
			notExpect: "Jane Doe",
		},
		{
			name:      "On ... wrote: quote removal",
			input:     "New reply content\n\nOn Mon, Apr 13, 2026 at 10:00 AM User <user@example.com> wrote:\n> Quoted text",
			expect:    "New reply content",
			notExpect: "Quoted text",
		},
		{
			name:      "iPhone signature removal",
			input:     "Checking in from the road.\n\nSent from my iPhone",
			expect:    "Checking in from the road.",
			notExpect: "iPhone",
		},
		{
			name:      "Nested quotes",
			input:     "Bottom reply\n\n> Quote level 1\n>> Quote level 2",
			expect:    "Bottom reply",
			notExpect: "Quote level 1",
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
		{"송현빈 <wisebean@goggle.com>", "송현빈"},                        // Korean name (Plain UTF-8)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := types.ExtractNameFromEmail(tt.input)
			if got != tt.expected {
				t.Errorf("ExtractNameFromEmail(%q) = %q; want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestUpsertAddresses(t *testing.T) {
	// Setup test DB for store dependency
	store.ResetForTest()
	dbURL := "file:./test.db?_busy_timeout=5000"
	
	// Use store's initialization but with our test URL
	store.InitDB(context.Background(), &config.Config{TursoURL: dbURL})
	store.InitContactsTable(context.Background(), store.GetDB())

	tenant := "tenant@whatap.io"
	
	// Case 1: Multiple recipients
	header := "Lim Sola <sola@whatap.io>, Kenny Holmes <kenny@whatap.io>"
	first := upsertAddresses(tenant, header, "gmail")

	if first != "sola@whatap.io" {
		t.Errorf("Expected sola@whatap.io, got %s", first)
	}
	
	// Check if both were registered in store (via NormalizeContactName)
	if name := store.NormalizeContactName(tenant, "sola@whatap.io"); name != "Lim Sola" {
		t.Errorf("Lim Sola not registered correctly: %s", name)
	}
	if name := store.NormalizeContactName(tenant, "kenny@whatap.io"); name != "Kenny Holmes" {
		t.Errorf("Kenny Holmes not registered correctly: %s", name)
	}
}

func TestIsMarketingHeader(t *testing.T) {
	tests := []struct {
		name     string
		headers  []*gmail.MessagePartHeader
		expected bool
	}{
		{
			name: "List-Unsubscribe exists",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<mailto:unsubscribe@example.com>"},
				{Name: "Subject", Value: "Promo"},
			},
			expected: true,
		},
		{
			name: "Precedence bulk",
			headers: []*gmail.MessagePartHeader{
				{Name: "Precedence", Value: "bulk"},
			},
			expected: true,
		},
		{
			name: "Precedence list",
			headers: []*gmail.MessagePartHeader{
				{Name: "Precedence", Value: "LIST"},
			},
			expected: true,
		},
		{
			name: "Normal Email",
			headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: "boss@example.com"},
				{Name: "Subject", Value: "Report"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMarketingHeader(tt.headers); got != tt.expected {
				t.Errorf("isMarketingHeader() = %v; want %v", got, tt.expected)
			}
		})
	}
}

