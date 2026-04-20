package ai

import (
	"testing"

	"github.com/google/generative-ai-go/genai"
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
	t.Parallel()
	parsed := LoadPrompt("gmail_system.prompt")
	if parsed == nil || parsed.Body == "" {
		t.Error("Expected 'gmail_system.prompt' to load successfully, got empty result")
	}
	if parsed.Meta.Name == "" {
		t.Error("Expected 'gmail_system.prompt' to have metadata name")
	}
}
