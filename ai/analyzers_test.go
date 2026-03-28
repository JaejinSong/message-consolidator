package ai

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzersPreProcess(t *testing.T) {
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
		t.Run(tt.name, func(t *testing.T) {
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
		t.Run(tt.source, func(t *testing.T) {
			analyzer := getAnalyzer(tt.source)
			if reflect.TypeOf(analyzer) != tt.expectedType {
				t.Errorf("getAnalyzer() for source '%s' returned type %T, want %v", tt.source, analyzer, tt.expectedType)
			}
		})
	}
}
