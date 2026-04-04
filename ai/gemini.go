package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
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

func NewGeminiClient(ctx context.Context, apiKey string, analysisModel, translationModel string, opts ...option.ClientOption) (*GeminiClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	logger.Infof("[GEMINI] Initializing client (Key length: %d, Prefix: %s..., Analysis: %s, Translation: %s)",
		len(apiKey), apiKey[:4], analysisModel, translationModel)

	allOpts := append([]option.ClientOption{option.WithAPIKey(apiKey)}, opts...)
	client, err := genai.NewClient(ctx, allOpts...)
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

// Why: Safely retries AI API calls with exponential backoff to handle transient errors and rate limits gracefully, ensuring reliability under high load.
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
			return nil, ctx.Err() //Why: Exits immediately if the context was canceled by the caller (e.g. timeout or client disconnect) to avoid redundant retry attempts.
		}

		logger.Warnf("[GEMINI] API call failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
		if attempt < maxRetries {
			// Why: Adds random jitter to the exponential backoff to prevent synchronized retries (thundering herd) and improve reliability against rate limits.
			backoff := time.Duration(1<<attempt) * time.Second
			jitter := time.Duration(float64(backoff) * (0.5 + 0.5*rand.Float64()))
			time.Sleep(jitter + 1*time.Second)
		}
	}
	return nil, fmt.Errorf("all %d attempts failed, last error: %w", maxRetries+1, err)
}

// Why: Extracts and records token consumption from the AI response for cost monitoring and precise performance tracing.
func logTokenUsage(ctx context.Context, email, stepName string, resp *genai.GenerateContentResponse) {
	if resp == nil || resp.UsageMetadata == nil {
		return
	}

	pTokens := int(resp.UsageMetadata.PromptTokenCount)
	cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
	store.AddTokenUsage(email, pTokens, cTokens)
	trace.Step(ctx, fmt.Sprintf("TokenUsage-%s (Prompt: %d, Comp: %d)", stepName, pTokens, cTokens), "", 0, 0)
}

// Why: Safely extracts the response text from the Gemini API candidates, handling empty or blocked responses gracefully.
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

// Why: Summarizes a list of tasks into a structured Markdown business report.
// It focuses purely on text generation, offloading visualization data processing to the backend for better efficiency.
func (g *GeminiClient) GenerateReportSummary(ctx context.Context, email string, tasks string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := loadPrompt("report_summary.prompt")
	// Why: [Unified Rendering] Validates and renders the report summary prompt using the standard template engine.
	data := ExtractionContext{
		MessagePayload: tasks,
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:         "English",
	}
	rendered, err := parsed.Render(data)
	if err != nil {
		return "", fmt.Errorf("failed to render report summary prompt: %w", err)
	}

	model := g.initModel(g.getEffectiveModel(parsed, g.analysisModel), 0.1, 4096, "", rendered)

	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(""), 60*time.Second, 2)
	if err != nil {
		return "", err
	}

	logTokenUsage(ctx, email, "ReportSummary", resp)
	text, err := extractResponseText(resp)
	if err != nil {
		return "", err
	}

	trace.Step(ctx, "Gemini-ReportSummary", "", int(time.Since(start).Milliseconds()), 0)
	return text, nil
}

func (g *GeminiClient) Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error) {
	tasks, _ := store.GetActiveContextTasks(ctx, email, source, room)
	return g.AnalyzeWithContext(ctx, email, msg, language, source, room, tasks)
}

func (g *GeminiClient) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	lang := g.getValidLang(language)
	analyzer := getAnalyzer(source)
	modelName := g.getAnalyzeModelName(analyzer)
	existingTasksJSON := g.marshalTasksForAI(tasks)
	userName, _ := store.GetUserName(email)
	if userName == "" {
		userName = email
	}

	// Why: [Contextual Extraction] Consolidates prompt data into a unified ExtractionContext for template rendering.
	data := ExtractionContext{
		MessagePayload:      msg.RawContent,
		CurrentTime:         time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:              lang,
		ExistingTasksJSON:   existingTasksJSON,
		EnrichedMessageJSON: g.marshalEnrichedMessage(msg),
		CurrentUser:        userName,
	}
	if analyzer != nil {
		data.MessagePayload = analyzer.PreProcess(data.MessagePayload)
	}

	model := g.initModel(modelName, 0.0, 4096, "application/json", g.getAnalyzeSysInst(analyzer, data))
	prompt := g.getAnalyzeUserPrompt(analyzer, data)

	logger.Infof("[GEMINI] Analyzing with %d context tasks (%s:%s)...", len(tasks), source, room)
	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 45*time.Second, 2)
	if err != nil {
		logger.Errorf("[GEMINI] Analysis failed: %v", err)
		return nil, err
	}

	raw, _ := extractResponseText(resp)
	// Why: Asynchronously logs AI inference data to both DB and file system to support Data Flywheel without blocking the main workflow.
	go func(src, input, output string) {
		logger.LogAIInferenceToFile(src, input, output)
		_ = store.LogAIInference(0, src, input, output)
	}(source, msg.RawContent, raw)

	trace.Step(ctx, "Gemini-Analyze", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Analyze", resp)
	return g.parseAnalyzeResults(resp)
}

func (g *GeminiClient) marshalEnrichedMessage(msg types.EnrichedMessage) string {
	b, _ := json.Marshal(msg)
	return string(b)
}

func (g *GeminiClient) marshalTasksForAI(tasks []store.ConsolidatedMessage) string {
	if len(tasks) == 0 {
		return "[]"
	}
	// Why: Simplified JSON for AI context to save tokens and improve extraction accuracy.
	type contextTask struct {
		ID       int    `json:"id"`
		Task     string `json:"task"`
		Original string `json:"original_text"`
		Source   string `json:"source"`
		Room     string `json:"room"`
		ThreadID string `json:"thread_id"`
	}
	var ctxTasks []contextTask
	for _, t := range tasks {
		ctxTasks = append(ctxTasks, contextTask{
			ID: t.ID, Task: t.Task, Original: t.OriginalText,
			Source: t.Source, Room: t.Room, ThreadID: t.ThreadID,
		})
	}
	b, _ := json.Marshal(ctxTasks)
	return string(b)
}


func (g *GeminiClient) Translate(ctx context.Context, email string, tasks []store.TranslateRequest, language string) ([]store.TranslateRequest, error) {
	if g == nil || g.client == nil || len(tasks) == 0 {
		return nil, fmt.Errorf("invalid translate request")
	}

	parsed := loadPrompt("translation_system.prompt")
	data := ExtractionContext{
		Locale:      g.getValidLang(language),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.0, 4096, "application/json", sysInst)

	logger.Debugf("[GEMINI] Translating %d tasks to %s...", len(tasks), language)
	start := time.Now()
	tasksJSON, _ := json.Marshal(tasks)
	resp, err := generateWithRetry(ctx, model, genai.Text(string(tasksJSON)), 30*time.Second, 2)
	if err != nil {
		logger.Errorf("[GEMINI] Translation failed: %v", err)
		return nil, err
	}

	trace.Step(ctx, "Gemini-Translate", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Translate", resp)

	rawJSON, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}
	return unmarshalTranslate(sanitizeJSON(rawJSON), rawJSON, language)
}

// Why: Translates a complete Markdown report into a target language while strictly preserving the structure.
// Uses the lightweight Flash-Lite model for maximum cost efficiency.
func (g *GeminiClient) TranslateReport(ctx context.Context, email string, reportInEnglish string, targetLanguage string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := loadPrompt("report_translator.prompt")
	data := ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.2, 4096, "", sysInst)

	logger.Debugf("[GEMINI] Translating Markdown report for %s to %s...", email, targetLanguage)
	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(reportInEnglish), 45*time.Second, 2)
	if err != nil {
		logger.Errorf("[GEMINI] Report translation failed (%s): %v", targetLanguage, err)
		return "", err
	}

	trace.Step(ctx, "Gemini-TranslateReport", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "TranslateReport", resp)
	return extractResponseText(resp)
}

// TranslateTaskMessage translates short, conversational messages (Slack, WhatsApp, Email).
// It use a specialized prompt to maintain tone and prevent unnecessary formatting (e.g., markdown bloat).
func (g *GeminiClient) TranslateTaskMessage(ctx context.Context, email string, text string, targetLanguage string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := loadPrompt("task_translator.prompt")
	data := ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.1, 0, "", sysInst)

	logger.Debugf("[GEMINI] Translating Task for %s to %s...", email, targetLanguage)
	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(text), 30*time.Second, 2)
	if err != nil {
		logger.Errorf("[GEMINI] Task translation failed (%s): %v", targetLanguage, err)
		return "", err
	}

	trace.Step(ctx, "Gemini-TranslateTask", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "TranslateTask", resp)
	return extractResponseText(resp)
}

// TranslateBatchTasks translates multiple tasks at once following the Page-unit Pure JIT pattern.
// Why: Minimizes AI calls and costs by batching N tasks into a single structured prompt.
func (g *GeminiClient) TranslateBatchTasks(ctx context.Context, email string, tasks []store.TranslateRequest, lang string) ([]store.TranslateRequest, error) {
	if len(tasks) == 0 { return nil, nil }
	parsed := loadPrompt("batch_translator.prompt")
	data := ExtractionContext{
		Locale:      g.getValidLang(lang),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.1, 4096, "application/json", sysInst)
	
	tasksJSON, _ := json.Marshal(tasks)
	resp, err := generateWithRetry(ctx, model, genai.Text(string(tasksJSON)), 45*time.Second, 2)
	if err != nil { return nil, err }
	
	logTokenUsage(ctx, email, "BatchTranslate", resp)
	raw, _ := extractResponseText(resp)
	var results []store.TranslateRequest
	if err := json.Unmarshal([]byte(sanitizeJSON(raw)), &results); err != nil {
		return nil, fmt.Errorf("failed to parse partial AI response: %w", err)
	}
	return results, nil
}



// --- Internal Helpers ---

func (g *GeminiClient) initModel(modelName string, temp float64, tokens int32, mime string, sys string) *genai.GenerativeModel {
	model := g.client.GenerativeModel(modelName)
	model.SafetySettings = relaxedSafetySettings
	model.SetTemperature(float32(temp))
	model.SetMaxOutputTokens(tokens)
	if mime != "" {
		model.ResponseMIMEType = mime
	}
	if sys != "" {
		model.SystemInstruction = &genai.Content{Parts: []genai.Part{genai.Text(sys)}}
	}
	return model
}

func (g *GeminiClient) getEffectiveModel(p *ParsedPrompt, def string) string {
	if p != nil && p.Meta.Model != "" {
		return p.Meta.Model
	}
	return def
}

func (g *GeminiClient) getValidLang(lang string) string {
	if lang == "" {
		return "Korean"
	}
	return lang
}

func (g *GeminiClient) getAnalyzeModelName(analyzer SourceAnalyzer) string {
	if analyzer != nil {
		return analyzer.GetModelName(g.analysisModel)
	}
	return g.analysisModel
}

func (g *GeminiClient) getAnalyzeSysInst(analyzer SourceAnalyzer, data ExtractionContext) string {
	if analyzer != nil {
		return analyzer.GetSystemInstruction(data)
	}
	return `Extract tasks as JSON array: [{"id", "state", "task", "requester", "assignee", "assigned_at", "source_ts", "deadline", "category"}]`
}

func (g *GeminiClient) getAnalyzeUserPrompt(analyzer SourceAnalyzer, data ExtractionContext) string {
	if analyzer != nil {
		return analyzer.GetUserPrompt(data)
	}
	return data.MessagePayload
}

func (g *GeminiClient) parseAnalyzeResults(resp *genai.GenerateContentResponse) ([]store.TodoItem, error) {
	raw, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}
	fmt.Printf("[DEBUG-GEMINI] RAW: %s\n", raw)
	clean := sanitizeJSON(raw)
	if clean == "" || clean == "[]" {
		return nil, nil
	}
	items, err := unmarshalAnalyze(clean, raw)
	if err != nil {
		return nil, err
	}

	// Why: Filters out 'none' state results which are informational/placeholder items from the AI context-ware extraction.
	// This ensures that only actionable tasks are returned, maintaining compatibility with pure extraction tests.
	var filtered []store.TodoItem
	for _, item := range items {
		if strings.ToLower(item.State) != "none" {
			filtered = append(filtered, item)
		}
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	return store.ConsolidateTasks(filtered), nil
}

func (g *GeminiClient) callGenericAPI(ctx context.Context, modelName, prompt string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	model := g.client.GenerativeModel(modelName)
	model.SafetySettings = relaxedSafetySettings
	model.SetTemperature(0.1)

	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 30*time.Second, 2)
	if err != nil {
		return "", err
	}

	return extractResponseText(resp)
}

// CallGenericAPI is a public wrapper for callGenericAPI.
func (g *GeminiClient) CallGenericAPI(ctx context.Context, modelName, prompt string) (string, error) {
	return g.callGenericAPI(ctx, modelName, prompt)
}
