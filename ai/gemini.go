package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"message-consolidator/ai/core"
	"message-consolidator/internal/safego"
	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"regexp"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
	"google.golang.org/genai"
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
	Status         string          `json:"status"`                    // NEW, UPDATE, RESOLVE, NONE
	UpdatedText    string          `json:"updated_text"`              // New English summary for UPDATE status
	SubtaskUpdates []SubtaskUpdate `json:"subtask_updates,omitempty"` // Partial subtask state changes for UPDATE/NONE
}

// SubtaskUpdate represents a single subtask state change returned by AI.
type SubtaskUpdate struct {
	Index int  `json:"index"`
	Done  bool `json:"done"`
}

const (
	// DefaultMaxTokens is the standard output limit for short-form analysis tasks.
	DefaultMaxTokens = 8192
	// ReportMaxTokens caps total response (thinking + output) for long-form reports.
	// Why: Gemini 3 Flash is a thinking model — `max_output_tokens` budgets both internal
	// reasoning AND visible completion. 8192 starved the visible output to ~300 tokens
	// (thinking ate ~7800), truncating activity tables. 40960 leaves ~24K for thinking
	// plus ~16K headroom for larger inputs. Don't drop without auditing thinking-token
	// consumption, otherwise activity/insight sections silently truncate.
	ReportMaxTokens = 40960
)

var relaxedSafetySettings = []*genai.SafetySetting{
	{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
	{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
	{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdBlockNone},
	{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdBlockNone},
}

type GeminiClient struct {
	client           *genai.Client
	analysisModel    string
	translationModel string
}

func NewGeminiClient(ctx context.Context, apiKey string, analysisModel, translationModel string, cfgOpts ...func(*genai.ClientConfig)) (*GeminiClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	logger.Infof("[GEMINI] Initializing client (Key length: %d, Prefix: %s..., Analysis: %s, Translation: %s)",
		len(apiKey), apiKey[:4], analysisModel, translationModel)

	// Why: ClientConfig.APIKey and HTTPClient are orthogonal in the new SDK —
	// the SDK injects the key via its own auth layer, so only a plain
	// WhaTap-wrapped client is needed (no apiKeyTransport shim required).
	cfg := &genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: whataphttpx.Client(),
	}
	for _, opt := range cfgOpts {
		opt(cfg)
	}
	client, err := genai.NewClient(ctx, cfg)
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
	MessageID store.MessageID `json:"message_id"`
	Text      string          `json:"translated_text"`
	Error     string          `json:"error,omitempty"`
}

// Why: Safely retries AI API calls with exponential backoff to handle transient errors and rate limits gracefully, ensuring reliability under high load.
func generateWithRetry(ctx context.Context, client *genai.Client, modelName string, contents []*genai.Content, cfg *genai.GenerateContentConfig, timeout time.Duration, maxRetries int) (*genai.GenerateContentResponse, error) {
	var resp *genai.GenerateContentResponse
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		apiCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err = client.Models.GenerateContent(apiCtx, modelName, contents, cfg)
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
			jitter := time.Duration(float64(backoff) * (0.5 + 0.5*rand.Float64())) //nolint:gosec // Jitter does not need cryptographic strength; math/rand suffices.
			time.Sleep(jitter + 1*time.Second)
		}
	}
	return nil, fmt.Errorf("all %d attempts failed, last error: %s", maxRetries+1, maskAPIKey(err))
}

// logTokenUsage records prompt/completion tokens against a (step, model, source, report_id)
// bucket so downstream dashboards can attribute cost. Pass source="" for steps that aren't
// bound to a specific channel (reports, translation, merge, etc.) and reportID=0 for steps
// not bound to a specific report.
func logTokenUsage(ctx context.Context, email, step, model, source string, reportID store.ReportID, resp *genai.GenerateContentResponse) {
	if resp == nil || resp.UsageMetadata == nil {
		return
	}

	pTokens := int(resp.UsageMetadata.PromptTokenCount)
	cTokens := int(resp.UsageMetadata.CandidatesTokenCount)
	tTokens := int(resp.UsageMetadata.ThoughtsTokenCount)
	if err := store.AddTokenUsage(email, step, model, source, reportID, pTokens, cTokens, tTokens); err != nil {
		logger.Warnf("[TOKEN-USAGE] %s/%s: %v", email, step, err)
	}
	// Why: tTokens is passed as value so WhaTap MXQL can query thinking-token consumption numerically.
	_ = trace.Step(ctx, fmt.Sprintf("TokenUsage-%s (Prompt: %d, Comp: %d, Think: %d)", step, pTokens, cTokens, tTokens), "", 0, tTokens)
}

// Why: Safely extracts the response text from the Gemini API candidates, handling empty or blocked responses gracefully.
func extractResponseText(resp *genai.GenerateContentResponse) (string, error) {
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty or blocked response from Gemini")
	}
	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		text += part.Text
	}
	return text, nil
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
	analyzer := core.GetAnalyzer(source)
	modelName := g.getAnalyzeModelName(analyzer)
	cfg := g.buildConfig(0.0, DefaultMaxTokens, "application/json", g.getAnalyzeSysInst(analyzer, data))
	cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(int32(3072))}
	prompt := g.getAnalyzeUserPrompt(analyzer, data)

	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text(prompt), cfg, 45*time.Second, 2)
	if err != nil {
		return nil, err
	}

	raw, _ := extractResponseText(resp)
	g.logInferenceAsync(source, msg.RawContent, raw) //nolint:contextcheck // Fire-and-forget filesystem log; ctx not applicable.

	_ = trace.Step(ctx, "Gemini-Analyze", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Analyze", modelName, source, 0, resp)

	candidates, err := g.parseAnalyzeResults(resp, data.CurrentUserID, data.CurrentUserEmail)
	if err != nil {
		return nil, err
	}

	return candidates, nil
}

func (g *GeminiClient) prepareAnalysisData(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string, tasks []store.ConsolidatedMessage) core.ExtractionContext {
	user, _ := store.GetOrCreateUser(ctx, email, "", "")
	userName := user.Name
	if userName == "" {
		userName = email
	}
	data := core.ExtractionContext{
		MessagePayload:      msg.RawContent,
		CurrentTime:         time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:              g.getValidLang(language),
		ExistingTasksJSON:   g.marshalTasksForAI(tasks),

		CurrentUser:         userName,
		CurrentUserEmail:    user.Email,
		CurrentUserID:       user.ID,
		ChatType:            msg.ChatType,
		RoomName:            room,
	}
	if analyzer := core.GetAnalyzer(source); analyzer != nil {
		data.MessagePayload = analyzer.PreProcess(data.MessagePayload)
	}
	return data
}

func (g *GeminiClient) logInferenceAsync(source, input, output string) {
	go func() {
		defer safego.Recover("ai-log-inference")
		logger.LogAIInferenceToFile(source, input, output)
		_ = store.LogAIInference(0, source, input, output)
	}()
}


func (g *GeminiClient) marshalTasksForAI(tasks []store.ConsolidatedMessage) string {
	if len(tasks) == 0 {
		return "[]"
	}
	// Why: source/room are invariant (query filters by both). original_text truncated to 120 runes to
	// preserve topical matching without shipping full history. requester/assignee/category/assigned_at/done
	// added so AI can resolve state (update/resolve/new) directly — eliminates the main source of
	// excessive thinking-token consumption.
	type contextTask struct {
		ID         store.MessageID `json:"id"`
		Task       string          `json:"task"`
		Original   string          `json:"original_text,omitempty"`
		Requester  string          `json:"requester,omitempty"`
		Assignee   string          `json:"assignee,omitempty"`
		Category   string          `json:"category,omitempty"`
		AssignedAt string          `json:"assigned_at,omitempty"`
		Done       bool            `json:"done"`
	}
	ctxTasks := make([]contextTask, 0, len(tasks))
	for _, t := range tasks {
		var assignedAt string
		if !t.AssignedAt.IsZero() {
			assignedAt = t.AssignedAt.UTC().Format(time.RFC3339)
		}
		ctxTasks = append(ctxTasks, contextTask{
			ID:         t.ID,
			Task:       t.Task,
			Original:   truncateRunes(t.OriginalText, 120),
			Requester:  t.Requester,
			Assignee:   t.Assignee,
			Category:   t.Category,
			AssignedAt: assignedAt,
			Done:       t.Done,
		})
	}
	b, _ := json.Marshal(ctxTasks)
	return string(b)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// --- Internal Helpers ---

func (g *GeminiClient) buildConfig(temp float64, tokens int32, mime string, sys string) *genai.GenerateContentConfig {
	cfg := &genai.GenerateContentConfig{
		SafetySettings: relaxedSafetySettings,
	}
	if temp != 0 {
		cfg.Temperature = genai.Ptr(float32(temp))
	}
	if tokens > 0 {
		cfg.MaxOutputTokens = tokens
	}
	if mime != "" {
		cfg.ResponseMIMEType = mime
	}
	if sys != "" {
		cfg.SystemInstruction = genai.NewContentFromText(sys, "")
	}
	return cfg
}

func (g *GeminiClient) getEffectiveModel(p *core.ParsedPrompt, def string) string {
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

func (g *GeminiClient) getAnalyzeModelName(analyzer core.SourceAnalyzer) string {
	if analyzer != nil {
		return analyzer.GetModelName(g.analysisModel)
	}
	return g.analysisModel
}

func (g *GeminiClient) getAnalyzeSysInst(analyzer core.SourceAnalyzer, data core.ExtractionContext) string {
	if analyzer != nil {
		return analyzer.GetSystemInstruction(data)
	}
	return `Extract tasks as JSON array: [{"id", "state", "task", "requester", "assignee", "assigned_at", "source_ts", "deadline", "category"}]`
}

func (g *GeminiClient) getAnalyzeUserPrompt(analyzer core.SourceAnalyzer, data core.ExtractionContext) string {
	if analyzer != nil {
		return analyzer.GetUserPrompt(data)
	}
	return data.MessagePayload
}

func (g *GeminiClient) parseAnalyzeResults(resp *genai.GenerateContentResponse, currentUserID store.UserID, userEmail string) ([]store.TodoItem, error) {
	raw, err := extractResponseText(resp)
	if err != nil {
		return nil, err
	}
	logger.Debugf("[GEMINI] raw response: %s", raw)
	clean := core.SanitizeJSON(raw)
	if clean == "" || clean == "[]" {
		return nil, nil
	}
	items, err := core.UnmarshalAnalyze(clean, raw, userEmail, currentUserID)
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

// CallGenericAPI runs a one-shot generation against the given model and records token usage
// under the caller-provided (step, source) bucket. Used by lightweight pipelines (e.g. the
// lite noise filter) that don't go through the Analyze/Translate helpers.
func (g *GeminiClient) CallGenericAPI(ctx context.Context, email, step, source, modelName, prompt string) (string, error) {
	if g == nil || g.client == nil {
		return "", fmt.Errorf("Gemini client is not initialized")
	}

	cfg := &genai.GenerateContentConfig{
		SafetySettings: relaxedSafetySettings,
		Temperature:    genai.Ptr[float32](0.1),
	}

	start := time.Now()
	resp, err := generateWithRetry(ctx, g.client, modelName, genai.Text(prompt), cfg, 30*time.Second, 2)
	if err != nil {
		return "", err
	}

	_ = trace.Step(ctx, "Gemini-"+step, "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, step, modelName, source, 0, resp)
	return extractResponseText(resp)
}
