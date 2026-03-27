package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/logger"
	"message-consolidator/store"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/whatap/go-api/trace"
	"google.golang.org/api/option"
)

var relaxedSafetySettings = []*genai.SafetySetting{
	{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockNone},
	{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockNone},
	{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockNone},
	{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockNone},
}

type GeminiClient struct {
	client           *genai.Client
	analysisModel    string
	translationModel string
}

func NewGeminiClient(ctx context.Context, apiKey string, analysisModel, translationModel string) (*GeminiClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	logger.Infof("[GEMINI] Initializing client (Key length: %d, Prefix: %s..., Analysis: %s, Translation: %s)",
		len(apiKey), apiKey[:4], analysisModel, translationModel)

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

// generateWithRetry safely retries AI API calls with exponential backoff to handle transient errors and rate limits gracefully, ensuring reliability under high load.
func generateWithRetry(ctx context.Context, model *genai.GenerativeModel, prompt genai.Part, timeout time.Duration, maxRetries int) (*genai.GenerateContentResponse, error) {
	var resp *genai.GenerateContentResponse
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		apiCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err = model.GenerateContent(apiCtx, prompt)
		cancel()

		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err() // Exit immediately if the context was canceled by the caller (e.g. timeout or client disconnect)
		}

		logger.Warnf("[GEMINI] API call failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
		if attempt < maxRetries {
			time.Sleep(time.Duration(1<<attempt) * 2 * time.Second) // Exponential backoff: Wait 2s, 4s, etc., before retrying to prevent overwhelming the API
		}
	}
	return nil, fmt.Errorf("all %d attempts failed, last error: %w", maxRetries+1, err)
}

// logTokenUsage extracts and records token consumption from the AI response for cost monitoring and performance tracing.
func logTokenUsage(ctx context.Context, email, stepName string, resp *genai.GenerateContentResponse) {
	if resp != nil && resp.UsageMetadata != nil {
		pTokens := int(resp.UsageMetadata.PromptTokenCount)
		cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
		store.AddTokenUsage(email, pTokens, cTokens)
		trace.Step(ctx, fmt.Sprintf("TokenUsage-%s (Prompt: %d, Comp: %d)", stepName, pTokens, cTokens), "", 0, 0)
	}
}

// extractResponseText safely extracts the response text from the Gemini API, handling cases where the response is empty or blocked by safety settings.
func extractResponseText(resp *genai.GenerateContentResponse) (string, error) {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty or blocked response from Gemini")
	}
	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if t, ok := part.(genai.Text); ok {
			text += string(t)
		}
	}
	return text, nil
}

func (g *GeminiClient) Analyze(ctx context.Context, email, conversationText string, language string, source string) ([]store.TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	if language == "" {
		language = "Korean"
	}

	analyzer := getAnalyzer(source)
	modelName := g.analysisModel
	if analyzer != nil {
		modelName = analyzer.GetModelName(g.analysisModel)
	}

	model := g.client.GenerativeModel(modelName)
	model.SafetySettings = relaxedSafetySettings
	model.ResponseMIMEType = "application/json"
	// Token optimization: set explicit limits and low temperature for stability
	model.SetTemperature(0.0)      // Strictly deterministic to prevent pronoun hallucinations
	model.SetMaxOutputTokens(4096) // Allocate sufficient token space (expanded from 1500) to ensure large task extractions are not truncated

	sysInst := `Extract tasks as JSON array (Return [] if no actionable task): [{"task", "requester", "assignee", "assigned_at", "source_ts", "deadline", "category"}]`
	if analyzer != nil {
		sysInst = analyzer.GetSystemInstruction(language)
	}
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(sysInst)},
	}

	userPrompt := conversationText
	if analyzer != nil {
		processedText := analyzer.PreProcess(conversationText)
		userPrompt = analyzer.GetUserPrompt(email, processedText)
	}

	logger.Infof("[GEMINI] Analyzing conversation (%s) in %s using model %s...", source, language, modelName)

	start := time.Now()
	// Analysis logic: Uses a 45-second timeout and up to 2 retries since this handles the longest prompts and is most prone to delays.
	resp, err := generateWithRetry(ctx, model, genai.Text(userPrompt), 45*time.Second, 2)
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-Analyze", "", elapsed, 0)

	if err != nil {
		logger.Errorf("[GEMINI] Analysis failed: %v", err)
		return nil, err
	}

	logTokenUsage(ctx, email, "Analyze", resp)

	rawJSON, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}

	cleanJSON := sanitizeJSON(rawJSON)
	if cleanJSON == "" || cleanJSON == "[]" {
		return nil, nil
	}

	return unmarshalAnalyze(cleanJSON, rawJSON)
}

func (g *GeminiClient) Translate(ctx context.Context, email string, tasks []store.TranslateRequest, language string) ([]store.TranslateRequest, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	model := g.client.GenerativeModel(g.translationModel)
	model.SafetySettings = relaxedSafetySettings
	model.ResponseMIMEType = "application/json"
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(fmt.Sprintf(loadPrompt("translation_system.prompt"), language, language))},
	}

	logger.Debugf("[GEMINI] Translating %d tasks to %s...", len(tasks), language)

	start := time.Now()
	tasksJSON, _ := json.Marshal(tasks)
	// Translation logic: Uses a 30-second timeout and up to 2 retries for optimal responsiveness during multi-language conversion.
	resp, err := generateWithRetry(ctx, model, genai.Text(string(tasksJSON)), 30*time.Second, 2)
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-Translate", "", elapsed, 0)

	if err != nil {
		logger.Errorf("[GEMINI] Translation failed: %v", err)
		return nil, err
	}

	logTokenUsage(ctx, email, "Translate", resp)

	rawJSON, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}

	cleanJSON := sanitizeJSON(rawJSON)
	if cleanJSON == "" {
		return nil, fmt.Errorf("empty translation response")
	}

	return unmarshalTranslate(cleanJSON, rawJSON, language)
}

func (g *GeminiClient) DoesReplyCompleteTask(ctx context.Context, email, taskText, replyText string) (bool, error) {
	if g == nil || g.client == nil {
		return false, fmt.Errorf("Gemini client is not initialized")
	}

	// Uses the lightest and fastest model (Lite) for simple Yes/No classification to minimize API latency.
	model := g.client.GenerativeModel(g.translationModel)
	model.SafetySettings = relaxedSafetySettings
	model.SetTemperature(0.0) // Deterministic
	model.SetMaxOutputTokens(10)

	prompt := fmt.Sprintf(loadPrompt("completion_check.prompt"), taskText, replyText)

	logger.Debugf("[GEMINI] Checking completion for task: %s", taskText)

	start := time.Now()
	// Simple completion check: Uses a short 15-second timeout and up to 2 retries as the expected output is minimal.
	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 15*time.Second, 2)
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-CheckCompletion", "", elapsed, 0)

	if err != nil {
		return false, err
	}

	logTokenUsage(ctx, email, "CheckCompletion", resp)

	answer, err := extractResponseText(resp)
	if err != nil {
		return false, err
	}

	answer = strings.ToUpper(strings.TrimSpace(answer))
	logger.Debugf("[GEMINI] Completion check result: %s", answer)

	return strings.HasPrefix(answer, "YES"), nil
}

func (g *GeminiClient) CheckTasksBatch(ctx context.Context, email, replyText string, tasks []store.ConsolidatedMessage) ([]int, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	// Uses the lightweight model to quickly process batch array (ID) returns, maximizing throughput.
	model := g.client.GenerativeModel(g.translationModel)
	model.SafetySettings = relaxedSafetySettings
	model.SetTemperature(0.0)
	model.SetMaxOutputTokens(200)
	model.ResponseMIMEType = "application/json"

	var taskList strings.Builder
	for _, t := range tasks {
		taskList.WriteString(fmt.Sprintf("- ID: %d, Task: %s\n", t.ID, t.Task))
	}

	prompt := fmt.Sprintf(loadPrompt("batch_completion_check.prompt"), taskList.String(), replyText)

	logger.Debugf("[GEMINI] Batch checking %d tasks for reply: %s", len(tasks), replyText)

	start := time.Now()
	// Batch processing logic: Uses a 30-second timeout and up to 2 retries to handle multiple tasks efficiently.
	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 30*time.Second, 2)
	elapsed := int(time.Since(start).Milliseconds())
	trace.Step(ctx, "Gemini-BatchCheckCompletion", "", elapsed, 0)

	if err != nil {
		return nil, err
	}

	logTokenUsage(ctx, email, "BatchCheck", resp)

	rawJSON, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}

	cleanJSON := sanitizeJSON(rawJSON)
	if cleanJSON == "" || cleanJSON == "[]" {
		return nil, nil
	}

	var completedIDs []int
	if err := json.Unmarshal([]byte(cleanJSON), &completedIDs); err != nil {
		logger.Errorf("[GEMINI] Batch JSON unmarshal failed: %v, RAW: %s", err, rawJSON)
		return nil, err
	}

	return completedIDs, nil
}
