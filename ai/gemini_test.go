package ai

import (
	"testing"

	"github.com/google/generative-ai-go/genai"
)

func TestExtractResponseText(t *testing.T) {
	// 1. Nil response test
	_, err := extractResponseText(nil)
	if err == nil {
		t.Error("Expected error for nil response, got nil")
	}

	// 2. Empty candidates test
	_, err = extractResponseText(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{}})
	if err == nil {
		t.Error("Expected error for empty candidates, got nil")
	}

	// 3. Nil content test
	_, err = extractResponseText(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{Content: nil}}})
	if err == nil {
		t.Error("Expected error for nil content, got nil")
	}

	// 4. Valid parts test
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []genai.Part{
						genai.Text("Hello "),
						genai.Text("World!"),
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

func TestLoadPrompt(t *testing.T) {
	content := loadPrompt("gmail_system.prompt")
	if content == "" {
		t.Error("Expected 'gmail_system.prompt' to load successfully, got empty string")
	}
}
