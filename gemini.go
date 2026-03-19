package main

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)


type GeminiClient struct {
	client           *genai.Client
	analysisModel    string
	translationModel string
}

func NewGeminiClient(ctx context.Context, apiKey string, analysisModel, translationModel string) (*GeminiClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	if analysisModel == "" {
		analysisModel = "gemini-3-flash-preview"
	}
	if translationModel == "" {
		translationModel = "gemini-3.1-flash-lite-preview"
	}
	return &GeminiClient{
		client:           client,
		analysisModel:    analysisModel,
		translationModel: translationModel,
	}, nil
}

func (g *GeminiClient) Analyze(ctx context.Context, conversationText string, language string, source string) ([]store.TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	if language == "" {
		language = "Korean"
	}

	model := g.client.GenerativeModel(g.analysisModel) // Use Analysis Model
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(`Extract tasks as a JSON array: [{"task", "requester", "assignee", "assigned_at", "source_ts", "original_text"}]
1. "task": Concise task description in %s.
2. "requester", "assignee": Extract accurately from text. Use names/IDs as they appear.
3. "original_text": The literal original text of the message (single-line, no modification).
4. "source_ts": Find via [TS:timestamp].`, language))},
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

	logger.Infof("[GEMINI] Analyzing conversation (%s) in %s...", source, language)
	resp, err := model.GenerateContent(ctx, genai.Text(userPrompt))
	if err != nil {
		logger.Errorf("[GEMINI] Analysis failed: %v", err)
		return nil, err
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			rawJSON += string(t)
		}
	}

	var items []store.TodoItem
	if err := json.Unmarshal([]byte(rawJSON), &items); err != nil {
		logger.Errorf("[GEMINI] JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}
	logger.Infof("[GEMINI] Successfully extracted %d tasks", len(items))
	return items, nil
}

func (g *GeminiClient) Translate(ctx context.Context, tasks []store.TranslateRequest, language string) ([]store.TranslateRequest, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	model := g.client.GenerativeModel(g.translationModel) // Use Translation Model
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(`Translate tasks to %s. Return JSON: {"translations": [{"id", "text"}]}.
1. Use "original_text" if provided, else "text".
2. Resulting "text" must be in %s.
3. Preserve names as they appear in the source text.`, language, language))},
	}

	logger.Debugf("[GEMINI] Translating %d tasks to %s...", len(tasks), language)
	tasksJSON, _ := json.Marshal(tasks)
	resp, err := model.GenerateContent(ctx, genai.Text(string(tasksJSON)))
	if err != nil {
		logger.Errorf("[GEMINI] Translation failed: %v", err)
		return nil, err
	}

	var rawJSON string
	for _, part := range resp.Candidates[0].Content.Parts {
		if text, ok := part.(genai.Text); ok {
			rawJSON += string(text)
		}
	}

	var tr store.TranslateResponse
	if err := json.Unmarshal([]byte(rawJSON), &tr); err != nil {
		logger.Errorf("[GEMINI] Translation JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}
	logger.Debugf("[GEMINI] Successfully translated %d items to %s", len(tr.Translations), language)
	return tr.Translations, nil
}
