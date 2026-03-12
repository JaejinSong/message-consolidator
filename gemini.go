package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type TodoItem struct {
	Task       string `json:"task"`
	Requester  string `json:"requester"`
	Assignee   string `json:"assignee"`
	AssignedAt string `json:"assigned_at"`
	SourceTS   string `json:"source_ts"`
}

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

func (g *GeminiClient) Analyze(ctx context.Context, conversationText string, language string) ([]TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	if language == "" {
		language = "Korean"
	}

	model := g.client.GenerativeModel("gemini-3-flash-preview")
	model.ResponseMIMEType = "application/json"

	prompt := fmt.Sprintf(`Analyze the following conversation and extract To-do items. 
Return a JSON array of objects with fields: "task", "requester", "assignee", "assigned_at", "source_ts".
Translate content to %s.
Use the [TS:timestamp] tag to find "source_ts".

Conversation:
---
%s
---`, language, conversationText)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
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

type TranslateRequest struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
}

type TranslateResponse struct {
	Translations []TranslateRequest `json:"translations"`
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

	tasksJSON, _ := json.Marshal(tasks)
	prompt := fmt.Sprintf(`Translate the "text" field of each object in the following JSON array to %s. 
Return a JSON object with a "translations" field containing the array of objects with their original "id" and translated "text".

JSON:
%s`, language, string(tasksJSON))

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
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
