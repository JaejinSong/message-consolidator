package channels

import (
	"context"
	"message-consolidator/internal/testutil"
	"message-consolidator/services"
	"message-consolidator/store"
	"message-consolidator/types"
	"strings"
	"testing"
	"google.golang.org/api/gmail/v1"
)



func TestProcessGeminiItems_SourceTSAlignment(t *testing.T) {
	t.Parallel()

	user := store.User{Email: "test@example.com", Name: "Test User"}

	// msgMap has only B and C — A is missing (simulates AI SourceTS mismatch)
	msgMap := map[string]types.RawMessage{
		"ts-B": {ID: "ts-B", Sender: "sender@example.com", Text: "Task B"},
		"ts-C": {ID: "ts-C", Sender: "sender@example.com", Text: "Task C"},
	}
	classificationMap := map[string]string{"ts-B": CategoryMine, "ts-C": CategoryMine}
	toMap := map[string]string{}

	items := []store.TodoItem{
		{Task: "Task A", SourceTS: "ts-A", Category: "TASK"}, // SourceTS not in msgMap
		{Task: "Task B", SourceTS: "ts-B", Category: "TASK"},
		{Task: "Task C", SourceTS: "ts-C", Category: "TASK"},
	}

	result := processGeminiItems(user.Email, &user, nil, items, classificationMap, toMap, msgMap)

	if _, ok := result["ts-A"]; ok {
		t.Error("ts-A should be skipped (SourceTS not in msgMap)")
	}
	msgB, ok := result["ts-B"]
	if !ok {
		t.Fatal("ts-B should be present in result")
	}
	if !strings.Contains(msgB.Task, "Task B") {
		t.Errorf("ts-B task = %q, want to contain 'Task B'", msgB.Task)
	}
	msgC, ok := result["ts-C"]
	if !ok {
		t.Fatal("ts-C should be present in result")
	}
	if !strings.Contains(msgC.Task, "Task C") {
		t.Errorf("ts-C task = %q, want to contain 'Task C'", msgC.Task)
	}
}

func TestProcessGeminiItems_NoDuplicateOnSkip(t *testing.T) {
	t.Parallel()

	user := store.User{Email: "test@example.com", Name: "Test User"}
	msgMap := map[string]types.RawMessage{
		"ts-1": {ID: "ts-1", Sender: "a@example.com", Text: "Only item"},
	}

	// Two items but only one has a valid SourceTS
	items := []store.TodoItem{
		{Task: "Ghost Task", SourceTS: "ts-missing", Category: "TASK"},
		{Task: "Real Task", SourceTS: "ts-1", Category: "TASK"},
	}

	result := processGeminiItems(user.Email, &user, nil, items, map[string]string{}, map[string]string{}, msgMap)

	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
	if _, ok := result["ts-1"]; !ok {
		t.Error("ts-1 should be in result")
	}
}

func TestProcessGeminiItems_UsesServicesBuiltTask(t *testing.T) {
	t.Parallel()

	user := store.User{Email: "test@example.com", Name: "Tester"}
	msgMap := map[string]types.RawMessage{
		"ts-x": {ID: "ts-x", Sender: "from@example.com", Text: "hello"},
	}
	items := []store.TodoItem{{Task: "Do something", SourceTS: "ts-x", Category: "QUERY"}}

	result := processGeminiItems(user.Email, &user, nil, items, map[string]string{"ts-x": CategoryMine}, map[string]string{}, msgMap)

	msg, ok := result["ts-x"]
	if !ok {
		t.Fatal("expected ts-x in result")
	}
	if msg.Category == "" {
		t.Error("category should not be empty")
	}
	if msg.Source != "gmail" {
		t.Errorf("source = %q, want 'gmail'", msg.Source)
	}
}

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
	cleanup, err := testutil.SetupTestDB(store.InitDB, store.ResetForTest)
	if err != nil {
		t.Fatalf("failed to setup test DB: %v", err)
	}
	defer cleanup()

	tenant := "tenant@whatap.io"

	// Case 1: Multiple recipients — first email returned, both contacts registered
	header := "Lim Sola <sola@whatap.io>, Kenny Holmes <kenny@whatap.io>"
	first, _ := upsertAddresses(context.Background(), tenant, header, "gmail")

	if first != "sola@whatap.io" {
		t.Errorf("Expected sola@whatap.io, got %s", first)
	}
	if name := store.NormalizeContactName(tenant, "sola@whatap.io"); name != "Lim Sola" {
		t.Errorf("Lim Sola not registered correctly: %s", name)
	}
	if name := store.NormalizeContactName(tenant, "kenny@whatap.io"); name != "Kenny Holmes" {
		t.Errorf("Kenny Holmes not registered correctly: %s", name)
	}

	// Case 2: Empty header — must return empty strings without panic
	empty, _ := upsertAddresses(context.Background(), tenant, "", "gmail")
	if empty != "" {
		t.Errorf("Expected empty string for empty header, got %s", empty)
	}

	// Case 3: Single recipient without display name — email used as identifier
	plain := "noreply@whatap.io"
	single, _ := upsertAddresses(context.Background(), tenant, plain, "gmail")
	if single == "" {
		t.Errorf("Expected non-empty result for plain email header, got empty")
	}
}

func TestIsMarketingHeader(t *testing.T) {
	internal := []string{"whatap.io"}
	tests := []struct {
		name            string
		headers         []*gmail.MessagePartHeader
		internalDomains []string
		expected        bool
	}{
		{
			name: "List-Unsubscribe exists",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<mailto:unsubscribe@example.com>"},
				{Name: "Subject", Value: "Promo"},
			},
			internalDomains: internal,
			expected:        true,
		},
		{
			name: "Precedence bulk",
			headers: []*gmail.MessagePartHeader{
				{Name: "Precedence", Value: "bulk"},
			},
			internalDomains: internal,
			expected:        true,
		},
		{
			name: "Precedence list",
			headers: []*gmail.MessagePartHeader{
				{Name: "Precedence", Value: "LIST"},
			},
			internalDomains: internal,
			expected:        true,
		},
		{
			name: "Normal Email",
			headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: "boss@example.com"},
				{Name: "Subject", Value: "Report"},
			},
			internalDomains: internal,
			expected:        false,
		},
		{
			name: "Internal Google Group (bracketed List-ID) exempt",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<https://groups.google.com/...>"},
				{Name: "List-ID", Value: "WhaTap Indonesia <indonesia.whatap.io>"},
			},
			internalDomains: internal,
			expected:        false,
		},
		{
			name: "Internal List-ID bare domain exempt",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<mailto:x@y.com>"},
				{Name: "List-ID", Value: "all.whatap.io"},
			},
			internalDomains: internal,
			expected:        false,
		},
		{
			name: "External List-ID still filtered",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<mailto:unsubscribe@external.com>"},
				{Name: "List-ID", Value: "<news.external.com>"},
			},
			internalDomains: internal,
			expected:        true,
		},
		{
			name: "Internal List-ID overrides Precedence bulk",
			headers: []*gmail.MessagePartHeader{
				{Name: "Precedence", Value: "bulk"},
				{Name: "List-ID", Value: "<eng.whatap.io>"},
			},
			internalDomains: internal,
			expected:        false,
		},
		{
			name: "Empty internal domains — no exemption applied",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<x>"},
				{Name: "List-ID", Value: "<x.whatap.io>"},
			},
			internalDomains: nil,
			expected:        true,
		},
		{
			name: "Multiple internal domains — second matches",
			headers: []*gmail.MessagePartHeader{
				{Name: "List-Unsubscribe", Value: "<x>"},
				{Name: "List-ID", Value: "<team.whatap.com>"},
			},
			internalDomains: []string{"whatap.io", "whatap.com"},
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isMarketingHeader(tt.headers, tt.internalDomains); got != tt.expected {
				t.Errorf("isMarketingHeader() = %v; want %v", got, tt.expected)
			}
		})
	}
}

