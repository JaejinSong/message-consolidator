package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/whatap/go-api/trace"
	"google.golang.org/api/option"
)

var apiKeyPattern = regexp.MustCompile(`(key=)[^&"'\s]+`)

func maskAPIKey(err error) string {
	if err == nil {
		return ""
	}
	return apiKeyPattern.ReplaceAllString(err.Error(), "${1}***")
}

// TaskTransition represents the AI's decision on how a reply impacts a parent task.
type TaskTransition struct {
	Status      string `json:"status"`       // NEW, UPDATE, RESOLVE, NONE
	UpdatedText string `json:"updated_text"` // New English summary for UPDATE status
}

const (
	// DefaultMaxTokens is the standard output limit for short-form analysis tasks.
	DefaultMaxTokens = 8192
	// ReportMaxTokens is the expanded limit for long-form report generation tasks.
	ReportMaxTokens = 32768
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

// TranslationResult defines the standardized AI response schema for batch translation tasks.
// Why: Enables partial failure handling by tracking errors per-message instead of failing the entire batch.
type TranslationResult struct {
	MessageID int    `json:"message_id"`
	Text      string `json:"translated_text"`
	Error     string `json:"error,omitempty"`
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

		logger.Warnf("[GEMINI] API call failed (attempt %d/%d): %s", attempt+1, maxRetries+1, maskAPIKey(err))
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
func (g *GeminiClient) GenerateReportSummary(ctx context.Context, email string, tasks string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := LoadPrompt("report_summary.prompt")
	data := ExtractionContext{
		MessagePayload: tasks,
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:         "English",
	}
	rendered, err := parsed.Render(data)
	if err != nil {
		return "", fmt.Errorf("failed to render report summary prompt: %w", err)
	}

	model := g.initModel(g.getEffectiveModel(parsed, g.analysisModel), 0.1, ReportMaxTokens, "", rendered)

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

// EvaluateTaskTransition determines if a reply completes or updates a specific parent task.
// Why: [Thread-Aware Intelligence] Uses a specialized prompt to analyze the conversational relationship 
// between a parent message and its reply, enabling deterministic state transitions (RESOLVE/UPDATE).
func (g *GeminiClient) EvaluateTaskTransition(ctx context.Context, email, parentTask, replyText string) (TaskTransition, error) {
	if g == nil || g.client == nil {
		return TaskTransition{}, fmt.Errorf("Gemini client not initialized")
	}

	parsed := LoadPrompt("completion_check.prompt")
	data := ExtractionContext{
		ParentTask:     parentTask,
		MessagePayload: replyText,
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:         "Korean",
	}

	rendered, err := parsed.Render(data)
	if err != nil {
		return TaskTransition{}, fmt.Errorf("failed to render completion prompt: %w", err)
	}

	model := g.initModel(g.getEffectiveModel(parsed, g.analysisModel), 0.1, 1024, "application/json", rendered)
	resp, err := generateWithRetry(ctx, model, genai.Text(""), 30*time.Second, 2)
	if err != nil {
		return TaskTransition{}, err
	}

	logTokenUsage(ctx, email, "EvaluateTransition", resp)
	raw, err := extractResponseText(resp)
	if err != nil {
		return TaskTransition{}, err
	}

	var result TaskTransition
	if err := json.Unmarshal([]byte(sanitizeJSON(raw)), &result); err != nil {
		return TaskTransition{}, fmt.Errorf("failed to parse AI transition response: %w (raw: %s)", err, raw)
	}

	return result, nil
}

// GenerateVisualizationData extracts graph structural data using strict ResponseSchema enforcement.
// Why: [Hallucination Defense] Eliminates invalid JSON by forcing the model to adhere to a predefined schema.
func (g *GeminiClient) GenerateVisualizationData(ctx context.Context, email string, tasks string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client not initialized")
	}

	schema := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"nodes": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"id":       {Type: genai.TypeString},
						"name":     {Type: genai.TypeString},
						"value":    {Type: genai.TypeNumber},
						"category": {Type: genai.TypeString},
					},
					Required: []string{"id", "name", "value", "category"},
				},
			},
			"links": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"source": {Type: genai.TypeString},
						"target": {Type: genai.TypeString},
						"weight": {Type: genai.TypeNumber},
					},
					Required: []string{"source", "target", "weight"},
				},
			},
		},
		Required: []string{"nodes", "links"},
	}

	model := g.initModel(g.analysisModel, 0.0, 4096, "application/json", "Generate a JSON graph of task relations.")
	model.ResponseSchema = schema

	prompt := fmt.Sprintf("Extract task network graph (Nodes=People, Links=Handover/Mention) from these logs:\n%s", tasks)
	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 60*time.Second, 2)
	if err != nil {
		return "", err
	}

	logTokenUsage(ctx, email, "ReportVizData", resp)
	return extractResponseText(resp)
}

// GenerateMergedTaskTitle summarizes multiple task titles and messages into a single English title.
// Why: [Unified Consistency] Strictly enforces 30-character English limit via AI for unified task presentation.
func (g *GeminiClient) GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error) {
	if g == nil || g.client == nil { return "", fmt.Errorf("Gemini client not initialized") }

	parsed := LoadPrompt("task_merge_summary.prompt")
	data := ExtractionContext{
		MessagePayload: tasksJSON,
		CurrentTime:    time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:         "English",
	}

	rendered, err := parsed.Render(data)
	if err != nil { return "", fmt.Errorf("failed to render merge summary prompt: %w", err) }

	// Why: [Performance] gemini-3-flash-preview is used for high-speed, high-quality short-form summary.
	modelName := g.getEffectiveModel(parsed, "gemini-3-flash-preview")
	model := g.initModel(modelName, 0.1, 100, "", rendered)

	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(""), 10*time.Second, 1)
	if err != nil { return "", err }

	logTokenUsage(ctx, email, "MergeSummary", resp)
	text, err := extractResponseText(resp)
	if err != nil { return "", err }

	trace.Step(ctx, "Gemini-MergeSummary", "", int(time.Since(start).Milliseconds()), 0)
	return CleanMarkdownText(text), nil
}

func (g *GeminiClient) Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error) {
	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), email, source, room)
	return g.AnalyzeWithContext(ctx, email, msg, language, source, room, tasks)
}

func (g *GeminiClient) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("Gemini client is not initialized")
	}

	data := g.prepareAnalysisData(ctx, email, msg, language, source, room, tasks)
	analyzer := getAnalyzer(source)
	modelName := g.getAnalyzeModelName(analyzer)
	model := g.initModel(modelName, 0.0, DefaultMaxTokens, "application/json", g.getAnalyzeSysInst(analyzer, data))
	prompt := g.getAnalyzeUserPrompt(analyzer, data)

	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 45*time.Second, 2)
	if err != nil {
		return nil, err
	}

	raw, _ := extractResponseText(resp)
	g.logInferenceAsync(source, msg.RawContent, raw)

	trace.Step(ctx, "Gemini-Analyze", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Analyze", resp)

	candidates, err := g.parseAnalyzeResults(resp)
	if err != nil {
		return nil, err
	}

	return candidates, nil
}

func (g *GeminiClient) prepareAnalysisData(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string, tasks []store.ConsolidatedMessage) ExtractionContext {
	user, _ := store.GetOrCreateUser(ctx, email, "", "")
	userName := user.Name
	if userName == "" {
		userName = email
	}
	data := ExtractionContext{
		MessagePayload:      msg.RawContent,
		CurrentTime:         time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:              g.getValidLang(language),
		ExistingTasksJSON:   g.marshalTasksForAI(tasks),
		EnrichedMessageJSON: g.marshalEnrichedMessage(msg),
		CurrentUser:         userName,
		CurrentUserEmail:    user.Email,
		CurrentUserID:       user.ID,
	}
	if analyzer := getAnalyzer(source); analyzer != nil {
		data.MessagePayload = analyzer.PreProcess(data.MessagePayload)
	}
	return data
}

func (g *GeminiClient) logInferenceAsync(source, input, output string) {
	go func() {
		logger.LogAIInferenceToFile(source, input, output)
		_ = store.LogAIInference(0, source, input, output)
	}()
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

	model, prompt := g.prepareTranslateResources(language, tasks)
	start := time.Now()
	resp, err := generateWithRetry(ctx, model, genai.Text(prompt), 30*time.Second, 2)
	if err != nil {
		return nil, err
	}

	trace.Step(ctx, "Gemini-Translate", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Translate", resp)
	return g.parseTranslateResults(resp)
}

func (g *GeminiClient) prepareTranslateResources(lang string, requests []store.TranslateRequest) (*genai.GenerativeModel, string) {
	parsed := LoadPrompt("translation_system.prompt")
	sysInst, _ := parsed.Render(ExtractionContext{
		Locale:      g.getValidLang(lang),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	})
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.0, 4096, "application/json", sysInst)
	tasksJSON, _ := json.Marshal(requests)
	return model, string(tasksJSON)
}

func (g *GeminiClient) parseTranslateResults(resp *genai.GenerateContentResponse) ([]store.TranslateRequest, error) {
	raw, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}
	return unmarshalTranslate(sanitizeJSON(raw), raw, "")
}

// Why: Translates a complete Markdown report into a target language while strictly preserving the structure.
// Uses the lightweight Flash-Lite model for maximum cost efficiency.
func (g *GeminiClient) TranslateReport(ctx context.Context, email string, reportInEnglish string, targetLanguage string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	parsed := LoadPrompt("report_translator.prompt")
	data := ExtractionContext{
		Locale:      g.getValidLang(targetLanguage),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.2, ReportMaxTokens, "", sysInst)

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

	parsed := LoadPrompt("task_translator.prompt")
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

// TranslateTasksBatch translates multiple tasks at once following the Page-unit Pure JIT pattern.
// Why: Minimizes AI calls and costs by batching N tasks into a single structured prompt with a 25-item threshold.
func (g *GeminiClient) TranslateTasksBatch(ctx context.Context, email string, tasks []store.TranslateRequest, lang string) ([]TranslationResult, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	if len(tasks) > 25 {
		return g.translateInChunks(ctx, email, tasks, lang, 25)
	}

	parsed := LoadPrompt("batch_translator.prompt")
	data := ExtractionContext{
		Locale:      g.getValidLang(lang),
		CurrentTime: time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	sysInst, _ := parsed.Render(data)
	model := g.initModel(g.getEffectiveModel(parsed, g.translationModel), 0.1, DefaultMaxTokens, "application/json", sysInst)

	tasksJSON, _ := json.Marshal(tasks)
	resp, err := generateWithRetry(ctx, model, genai.Text(string(tasksJSON)), 45*time.Second, 3)
	if err != nil {
		return nil, err
	}

	logTokenUsage(ctx, email, "BatchTranslate", resp)
	raw, _ := extractResponseText(resp)
	var results []TranslationResult
	if err := json.Unmarshal([]byte(sanitizeJSON(raw)), &results); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	return results, nil
}

func (g *GeminiClient) translateInChunks(ctx context.Context, email string, tasks []store.TranslateRequest, lang string, chunkSize int) ([]TranslationResult, error) {
	var allResults []TranslationResult
	for i := 0; i < len(tasks); i += chunkSize {
		end := i + chunkSize
		if end > len(tasks) {
			end = len(tasks)
		}

		chunk, err := g.TranslateTasksBatch(ctx, email, tasks[i:end], lang)
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, chunk...)
	}
	return allResults, nil
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

	return store.DeduplicateTasks(filtered), nil
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
