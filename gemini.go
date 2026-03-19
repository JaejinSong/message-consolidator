package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// Types moved to types.go


type GeminiClient struct {
	client *genai.Client
}

func NewGeminiClient(ctx context.Context, apiKey string) (*GeminiClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &GeminiClient{client: client}, nil
}

func (g *GeminiClient) Analyze(ctx context.Context, conversationText string, language string, source string) ([]TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	if language == "" {
		language = "Korean"
	}

	model := g.client.GenerativeModel("gemini-3-flash-preview") // Use Gemini 3 Flash Preview model
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(`You are a task extraction expert.
Analyze the provided content and extract actual actionable tasks or requests.
Return a JSON array of objects with fields: "task", "requester", "assignee", "assigned_at", "source_ts", "original_text".
"task" MUST be in %s.
"requester" and "assignee" MUST be kept EXACTLY as they appear in the original text.
"original_text" MUST be a concise one-line summary of the original message in %s.
Use the [TS:timestamp] tag to find "source_ts".`, language, language))},
	}

	var userPrompt string
	switch source {
	case "gmail":
		userPrompt = fmt.Sprintf(`If the sender domain is NOT @whatap.io, EXCLUDE simple informational or notification content.
Emails:
%s`, conversationText)
	case "slack", "whatsapp":
		userPrompt = fmt.Sprintf(`Analyze this %s chat:
%s`, source, conversationText)
	default:
		userPrompt = conversationText
	}

	resp, err := model.GenerateContent(ctx, genai.Text(userPrompt))
	if err != nil {
		return nil, err
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			rawJSON += string(text)
		}
	}

	var items []TodoItem
	if err := json.Unmarshal([]byte(rawJSON), &items); err != nil {
		return nil, err
	}
	return items, nil
}



func (g *GeminiClient) Translate(ctx context.Context, tasks []TranslateRequest, language string) ([]TranslateRequest, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	model := g.client.GenerativeModel("gemini-3-flash-preview")
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(`You are a task translation expert.
Translate or re-summarize tasks into %s.
Output JSON: { "translations": [ { "id": number, "text": "translated or summarized text" } ] }
1. Use "original_text" if provided.
2. If missing, translate "text".
3. Output "text" MUST be strictly in %s.`, language, language))},
	}

	tasksJSON, _ := json.Marshal(tasks)
	resp, err := model.GenerateContent(ctx, genai.Text(string(tasksJSON)))
	if err != nil {
		return nil, err
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			rawJSON += string(text)
		}
	}

	var tr TranslateResponse
	if err := json.Unmarshal([]byte(rawJSON), &tr); err != nil {
		return nil, err
	}
	return tr.Translations, nil
}
