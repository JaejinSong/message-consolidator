package core

import (
	"message-consolidator/types"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAnalyzersPromptHooks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		analyzer SourceAnalyzer
	}{
		{"Gmail", &GmailAnalyzer{}},
		{"Chat", &ChatAnalyzer{Source: "slack", Window: time.Minute}},
		{"Notion", &NotionAnalyzer{}},
	}

	ctx := ExtractionContext{MessagePayload: "hello", CurrentUser: "tester"}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"_GetSystemInstruction", func(t *testing.T) {
			t.Parallel()
			if got := tc.analyzer.GetSystemInstruction(ctx); got == "" {
				t.Errorf("%s system instruction is empty", tc.name)
			}
		})
		t.Run(tc.name+"_GetUserPrompt", func(t *testing.T) {
			t.Parallel()
			if got := tc.analyzer.GetUserPrompt(ctx); got == "" {
				t.Errorf("%s user prompt is empty", tc.name)
			}
		})
		t.Run(tc.name+"_GetModelName_FallbackUsesDefault", func(t *testing.T) {
			t.Parallel()
			got := tc.analyzer.GetModelName("default-model")
			if got == "" {
				t.Errorf("%s model name is empty", tc.name)
			}
		})
	}
}

func TestGroupMessagesByTime(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(sender string, offsetSec int) types.RawMessage {
		return types.RawMessage{Sender: sender, Timestamp: base.Add(time.Duration(offsetSec) * time.Second)}
	}

	tests := []struct {
		name     string
		input    []types.RawMessage
		interval time.Duration
		groups   int
	}{
		{name: "Empty Returns Nil", input: nil, interval: time.Minute, groups: 0},
		{
			name: "Single Sender Within Window Forms One Group",
			input: []types.RawMessage{
				mk("alice", 0), mk("alice", 30), mk("alice", 50),
			},
			interval: time.Minute,
			groups:   1,
		},
		{
			name: "Sender Switch Splits Groups",
			input: []types.RawMessage{
				mk("alice", 0), mk("bob", 5), mk("alice", 10),
			},
			interval: time.Minute,
			groups:   3,
		},
		{
			name: "Same Sender Past Interval Splits",
			input: []types.RawMessage{
				mk("alice", 0), mk("alice", 120),
			},
			interval: time.Minute,
			groups:   2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GroupMessagesByTime(tt.input, tt.interval)
			if len(got) != tt.groups {
				t.Errorf("GroupMessagesByTime() groups = %d, want %d (got %#v)", len(got), tt.groups, got)
			}
		})
	}
}

func TestAnalyzersPreProcess(t *testing.T) {
	t.Parallel()
	longText16k := strings.Repeat("a", 16000)
	longText31k := strings.Repeat("b", 31000)

	tests := []struct {
		name     string
		analyzer SourceAnalyzer
		input    string
		expected string
	}{
		//Why: [Gmail] Verifies the Gmail-specific preprocessing and truncation logic.
		// GmailAnalyzer Tests
		{
			name:     "GmailAnalyzer - Short Text (No Change)",
			analyzer: &GmailAnalyzer{},
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "GmailAnalyzer - Long Text (Truncate from start)",
			analyzer: &GmailAnalyzer{},
			input:    longText16k,
			expected: longText16k[:15000],
		},
		//Why: [Chat] Verifies the Chat-specific preprocessing and truncation logic for Slack/WhatsApp.
		// ChatAnalyzer Tests
		{
			name:     "ChatAnalyzer - Short Text (No Change)",
			analyzer: &ChatAnalyzer{Source: "slack"},
			input:    "hello slack",
			expected: "hello slack",
		},
		{
			name:     "ChatAnalyzer - Long Text (Truncate from end)",
			analyzer: &ChatAnalyzer{Source: "whatsapp"},
			input:    longText31k,
			expected: longText31k[1000:], //Why: Calculates the expected offset for end-truncation to ensure at most 30,000 characters are preserved.
		},
		//Why: [Notion] Verifies that the Notion-specific preprocessing logic currently preserves the input as-is.
		// NotionAnalyzer Tests
		{
			name:     "NotionAnalyzer - Any Text (No Change)",
			analyzer: &NotionAnalyzer{},
			input:    "hello notion",
			expected: "hello notion",
		},
		{
			name:     "NotionAnalyzer - Long Text (No Truncate)",
			analyzer: &NotionAnalyzer{},
			input:    longText31k,
			expected: longText31k,
		},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.analyzer.PreProcess(tt.input)
			if got != tt.expected {
				t.Errorf("PreProcess() got = %v, want %v", got, tt.expected)
			}
			if len(got) != len(tt.expected) {
				t.Errorf("PreProcess() length got = %d, want %d", len(got), len(tt.expected))
			}
		})
	}
}

func TestGetAnalyzer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		source       string
		expectedType reflect.Type
	}{
		{"gmail", reflect.TypeOf(&GmailAnalyzer{})},
		{"slack", reflect.TypeOf(&ChatAnalyzer{})},
		{"whatsapp", reflect.TypeOf(&ChatAnalyzer{})},
		{"telegram", reflect.TypeOf(&ChatAnalyzer{})},
		{"notion", reflect.TypeOf(&NotionAnalyzer{})},
		{"unknown_source", nil},
	}

	for _, tt := range tests {
		tt := tt // Closure capture
		t.Run(tt.source, func(t *testing.T) {
			t.Parallel()
			analyzer := GetAnalyzer(tt.source)
			if reflect.TypeOf(analyzer) != tt.expectedType {
				t.Errorf("GetAnalyzer() for source '%s' returned type %T, want %v", tt.source, analyzer, tt.expectedType)
			}
		})
	}
}
