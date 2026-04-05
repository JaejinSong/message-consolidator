package services

import (
	"strings"
	"testing"
)

func TestExtractJSONBlock(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantErr      bool
		wantJSON     string
		wantStripped string
	}{
		{
			name: "Perfect Block",
			content: "Summary before\n```json\n{\"id\": 1}\n```\nInsight after",
			wantErr: false,
			wantJSON: "{\"id\": 1}",
			wantStripped: "Summary before\n\nInsight after",
		},
		{
			name: "No JSON Block",
			content: "Just plain text here.",
			wantErr: true,
			wantJSON: "",
			wantStripped: "Just plain text here.",
		},
		{
			name: "Unclosed Block",
			content: "Start here\n```json\n{\"id\": 1}\nSome content but no end mark",
			wantErr: true,
			wantJSON: "",
			wantStripped: "Start here\n```json\n{\"id\": 1}\nSome content but no end mark",
		},
		{
			name: "Empty JSON Block",
			content: "Start\n```json\n\n```\nEnd",
			wantErr: false,
			wantJSON: "",
			wantStripped: "Start\n\nEnd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonStr, stripped, err := ExtractJSONBlock(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExtractJSONBlock() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if jsonStr != tt.wantJSON {
				t.Errorf("ExtractJSONBlock() gotJSON = %v, want %v", jsonStr, tt.wantJSON)
			}
			// Clean up extra whitespace for comparison
			if strings.TrimSpace(stripped) != strings.TrimSpace(tt.wantStripped) {
				t.Errorf("ExtractJSONBlock() gotStripped = %v, want %v", stripped, tt.wantStripped)
			}
		})
	}
}

func TestExtractSection(t *testing.T) {
	content := "## [A]\nBody A\n## [B]\nBody B\nSome more text\n## [C]\nBody C"
	
	t.Run("Section A", func(t *testing.T) {
		got := ExtractSection(content, "## [A]")
		if got != "Body A" {
			t.Errorf("Expected 'Body A', got '%s'", got)
		}
	})

	t.Run("Section B", func(t *testing.T) {
		got := ExtractSection(content, "## [B]")
		if got != "Body B\nSome more text" {
			t.Errorf("Expected 'Body B\nSome more text', got '%s'", got)
		}
	})

	t.Run("Non-existent Section", func(t *testing.T) {
		got := ExtractSection(content, "## [D]")
		if got != "" {
			t.Errorf("Expected empty string, got '%s'", got)
		}
	})
}
