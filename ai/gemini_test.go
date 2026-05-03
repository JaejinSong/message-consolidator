package ai

import (
	"errors"
	"message-consolidator/ai/core"
	"testing"

	"google.golang.org/genai"
)

func TestExtractResponseText(t *testing.T) {
	t.Parallel()
	//Why: [Nil response test] Ensures that passing a nil response object results in a proper error during text extraction.
	_, err := extractResponseText(nil)
	if err == nil {
		t.Error("Expected error for nil response, got nil")
	}

	//Why: [Empty candidates test] Verifies that an empty candidates array is handled gracefully as an error.
	_, err = extractResponseText(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{}})
	if err == nil {
		t.Error("Expected error for empty candidates, got nil")
	}

	//Why: [Nil content test] Ensures that even if a candidate is present but its content is nil, an error is returned.
	_, err = extractResponseText(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: nil}}})
	if err == nil {
		t.Error("Expected error for nil content, got nil")
	}

	//Why: [Valid parts test] Confirms that multiple text parts in the Gemini response candidate are correctly concatenated.
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []*genai.Part{
						{Text: "Hello "},
						{Text: "World!"},
					},
				},
			},
		},
	}
	text, err := extractResponseText(resp)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if text != "Hello World!" {
		t.Errorf("Expected 'Hello World!', got '%s'", text)
	}
}

func TestMaskAPIKey(t *testing.T) {
	t.Parallel()
	if got := maskAPIKey(nil); got != "" {
		t.Errorf("maskAPIKey(nil) = %q, want empty", got)
	}
	plain := maskAPIKey(errors.New("no key here"))
	if plain != "no key here" {
		t.Errorf("maskAPIKey plain = %q", plain)
	}
	masked := maskAPIKey(errors.New("key=AIzaSyABC123DEF456GHI"))
	if masked == "" {
		t.Error("maskAPIKey should not return empty for error with key")
	}
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"안녕하세요", 3, "안녕하"},
		{"hello", 0, ""},
		{"", 5, ""},
	}
	for _, tt := range tests {
		if got := truncateRunes(tt.s, tt.max); got != tt.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}

func TestLoadPrompt(t *testing.T) {
	t.Parallel()
	parsed := core.LoadPrompt(core.PromptGmailSystem)
	if parsed == nil || parsed.Body == "" {
		t.Error("Expected 'gmail_system.prompt' to load successfully, got empty result")
	}
	if parsed.Meta.Name == "" {
		t.Error("Expected 'gmail_system.prompt' to have metadata name")
	}
}
