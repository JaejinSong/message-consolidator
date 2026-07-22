package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"message-consolidator/ai/core"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/store"
	"message-consolidator/types"
	"sort"
	"strings"
	"time"

	"github.com/whatap/go-api/trace"
	"google.golang.org/genai"
)

// Provider identifiers select the active text-generation backend.
const (
	providerGemini   = "gemini"
	providerDeepSeek = "deepseek"
)

const (
	// DefaultMaxTokens is the standard output limit for short-form analysis tasks.
	DefaultMaxTokens = 8192
	// ReportMaxTokens caps total response (thinking + output) for long-form reports.
	// Why: Gemini 3 Flash is a thinking model — `max_output_tokens` budgets both internal
	// reasoning AND visible completion. 8192 starved the visible output to ~300 tokens
	// (thinking ate ~7800), truncating activity tables. 40960 leaves ~24K for thinking
	// plus ~16K headroom for larger inputs. Don't drop without auditing thinking-token
	// consumption, otherwise activity/insight sections silently truncate.
	ReportMaxTokens = 65536
)

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

// TranslationResult defines the standardized AI response schema for batch translation tasks.
// Why: Enables partial failure handling by tracking errors per-message instead of failing the entire batch.
type TranslationResult struct {
	MessageID store.MessageID `json:"message_id"`
	Text      string          `json:"translated_text"`
	Error     string          `json:"error,omitempty"`
}

// modelSpec binds a per-stage model id to its thinking mode. Values are resolved
// per provider at construction time (see NewGeminiClient / newDeepSeekClient).
type modelSpec struct {
	model    string
	thinking ThinkingMode
}

// AIClient is the provider-neutral text-generation client. It owns all prompt
// building, parsing and token logging and delegates the single network call to
// an LLMTransport, so Gemini and DeepSeek share one code path.
type AIClient struct {
	transport   LLMTransport
	provider    string
	tracePrefix string // APM step label prefix ("Gemini" | "DeepSeek")

	analyze    modelSpec
	translate  modelSpec
	report     modelSpec
	transition modelSpec
	viz        modelSpec
	merge      modelSpec
	filter     modelSpec
	identity   modelSpec
}

// GeminiClient is the legacy name retained as an alias so existing call sites
// (scanner/services holding *ai.GeminiClient) compile unchanged. New code should
// reference AIClient directly.
type GeminiClient = AIClient

// ProviderConfig carries the settings needed to construct an AIClient for either
// provider; main.go and scanner build it from the application Config.
type ProviderConfig struct {
	Provider string // providerGemini (default) | providerDeepSeek

	GeminiAPIKey           string
	GeminiAnalysisModel    string
	GeminiTranslationModel string

	DeepSeekAPIKey           string
	DeepSeekBaseURL          string
	DeepSeekFilterModel      string
	DeepSeekAnalysisModel    string
	DeepSeekTranslationModel string
	DeepSeekReportModel      string
}

// Enabled reports whether the configured provider has the credentials needed to run.
// Callers gate AIClient construction on this (text generation is optional at boot).
func (pc ProviderConfig) Enabled() bool {
	if strings.EqualFold(pc.Provider, providerDeepSeek) {
		return strings.TrimSpace(pc.DeepSeekAPIKey) != ""
	}
	return strings.TrimSpace(pc.GeminiAPIKey) != ""
}

// NewAIClient builds the client for the configured provider. cfgOpts apply to the
// Gemini genai client only (used by tests to inject a fake backend).
func NewAIClient(ctx context.Context, pc ProviderConfig, cfgOpts ...func(*genai.ClientConfig)) (*AIClient, error) {
	if strings.EqualFold(pc.Provider, providerDeepSeek) {
		// Why: DeepSeek client construction is synchronous; whataphttpx.Client propagates the
		// trace via http.Request.Context at call time, so ctx is not threaded here (matches
		// the Gemini/embedding client pattern in internal/whataphttpx).
		return newDeepSeekClient(pc) //nolint:contextcheck // see comment above
	}
	return NewGeminiClient(ctx, pc.GeminiAPIKey, pc.GeminiAnalysisModel, pc.GeminiTranslationModel, cfgOpts...)
}

// NewGeminiClient constructs an AIClient backed by Gemini. Stage specs replicate
// the prior per-call model/thinking behavior exactly (prompt-meta model routing
// is still honored at call time via resolveModel).
func NewGeminiClient(ctx context.Context, apiKey, analysisModel, translationModel string, cfgOpts ...func(*genai.ClientConfig)) (*AIClient, error) {
	t, err := newGeminiTransport(ctx, apiKey, cfgOpts...)
	if err != nil {
		return nil, err
	}
	if analysisModel == "" {
		analysisModel = "gemini-3-flash-preview"
	}
	if translationModel == "" {
		translationModel = "gemini-3.1-flash-lite"
	}
	logger.Infof("[GEMINI] client ready (Analysis: %s, Translation: %s)", analysisModel, translationModel)
	return &AIClient{
		transport:   t,
		provider:    providerGemini,
		tracePrefix: "Gemini",
		analyze:     modelSpec{analysisModel, ThinkOn}, // was ThinkingBudget 3072
		translate:   modelSpec{translationModel, ThinkDefault},
		report:      modelSpec{analysisModel, ThinkOff}, // was ThinkingBudget 0
		transition:  modelSpec{analysisModel, ThinkDefault},
		viz:         modelSpec{analysisModel, ThinkDefault},
		merge:       modelSpec{analysisModel, ThinkDefault}, // prompt-meta overrides to gemini-3-flash-preview
		filter:      modelSpec{"", ThinkDefault},            // model resolved from lite_filter prompt meta
		identity:    modelSpec{analysisModel, ThinkOn},      // was ThinkingBudget 1024 (preview ignores cap)
	}, nil
}

func newDeepSeekClient(pc ProviderConfig) (*AIClient, error) {
	t, err := newDeepSeekTransport(pc.DeepSeekAPIKey, pc.DeepSeekBaseURL)
	if err != nil {
		return nil, err
	}
	filterModel := orDefault(pc.DeepSeekFilterModel, deepSeekChatModel)
	analysisModel := orDefault(pc.DeepSeekAnalysisModel, deepSeekChatModel)
	translationModel := orDefault(pc.DeepSeekTranslationModel, deepSeekChatModel)
	reportModel := orDefault(pc.DeepSeekReportModel, deepSeekProModel)
	logger.Infof("[DEEPSEEK] client ready (Filter: %s, Analysis: %s, Translation: %s, Report: %s)",
		filterModel, analysisModel, translationModel, reportModel)
	return &AIClient{
		transport:   t,
		provider:    providerDeepSeek,
		tracePrefix: "DeepSeek",
		analyze:     modelSpec{analysisModel, ThinkOff},
		translate:   modelSpec{translationModel, ThinkOff},
		report:      modelSpec{reportModel, ThinkOn},
		transition:  modelSpec{deepSeekReasonerModel, ThinkOn},
		viz:         modelSpec{deepSeekChatModel, ThinkOff},
		merge:       modelSpec{deepSeekChatModel, ThinkOff},
		filter:      modelSpec{filterModel, ThinkOff},
		identity:    modelSpec{deepSeekReasonerModel, ThinkOn},
	}, nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// resolveModel returns the effective model id for a stage: the prompt's per-provider
// frontmatter model (geminiModel/deepseekModel) if declared, else the code modelSpec
// (which carries env overrides + hardcoded defaults as a fallback).
func (g *AIClient) resolveModel(p *core.ParsedPrompt, spec modelSpec) string {
	if p != nil {
		if m := p.Meta.ModelFor(g.provider); m != "" {
			return m
		}
	}
	return spec.model
}

// resolveThinking returns the effective thinking mode for a stage: the prompt's
// per-provider frontmatter thinking (geminiThinking/deepseekThinking) if declared,
// else the code modelSpec default.
func (g *AIClient) resolveThinking(p *core.ParsedPrompt, spec modelSpec) ThinkingMode {
	if p != nil {
		if t := p.Meta.ThinkingFor(g.provider); t != "" {
			return parseThinkingMode(t, spec.thinking)
		}
	}
	return spec.thinking
}

func parseThinkingMode(s string, fallback ThinkingMode) ThinkingMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on":
		return ThinkOn
	case "off":
		return ThinkOff
	case "default":
		return ThinkDefault
	default:
		return fallback
	}
}

func (g *AIClient) Analyze(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string) ([]store.TodoItem, error) {
	tasks, _ := store.GetActiveContextTasks(ctx, store.GetDB(), email, source, room)
	return g.AnalyzeWithContext(ctx, email, msg, language, source, room, tasks)
}

func (g *AIClient) AnalyzeWithContext(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string, tasks []store.ConsolidatedMessage) ([]store.TodoItem, error) {
	if g == nil || g.transport == nil {
		return nil, fmt.Errorf("AI client is not initialized")
	}

	data := g.prepareAnalysisData(ctx, email, msg, language, source, room, tasks)
	analyzer := core.GetAnalyzer(source)
	var sysPrompt *core.ParsedPrompt
	if analyzer != nil {
		sysPrompt = core.LoadPrompt(analyzer.SystemPrompt())
	}
	modelName := g.resolveModel(sysPrompt, g.analyze)
	req := LLMRequest{
		Model:       modelName,
		System:      g.getAnalyzeSysInst(analyzer, data),
		User:        g.getAnalyzeUserPrompt(analyzer, data),
		Temperature: 0.0,
		MaxTokens:   DefaultMaxTokens,
		JSONMode:    true,
		Thinking:    g.resolveThinking(sysPrompt, g.analyze),
	}

	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 45*time.Second, 2)
	if err != nil {
		return nil, err
	}

	g.logInferenceAsync(source, msg.RawContent, resp.Text) //nolint:contextcheck // Fire-and-forget filesystem log; ctx not applicable.

	_ = trace.Step(ctx, g.tracePrefix+"-Analyze", "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, "Analyze", modelName, source, 0, resp.Usage)

	return g.parseAnalyzeResults(resp.Text, data.CurrentUserID, data.CurrentUserEmail)
}

func (g *AIClient) prepareAnalysisData(ctx context.Context, email string, msg types.EnrichedMessage, language string, source, room string, tasks []store.ConsolidatedMessage) core.ExtractionContext {
	user, _ := store.GetOrCreateUser(ctx, email, "", "")
	userName := user.Name
	if userName == "" {
		userName = email
	}
	data := core.ExtractionContext{
		MessagePayload:    msg.RawContent,
		CurrentTime:       time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:            g.getValidLang(language),
		ExistingTasksJSON: g.marshalTasksForAI(tasks),

		CurrentUser:      userName,
		CurrentUserEmail: user.Email,
		CurrentUserID:    user.ID,
		ChatType:         msg.ChatType,
		RoomName:         room,
	}
	if analyzer := core.GetAnalyzer(source); analyzer != nil {
		data.MessagePayload = analyzer.PreProcess(data.MessagePayload)
	}
	return data
}

func (g *AIClient) logInferenceAsync(source, input, output string) {
	go func() {
		defer safego.Recover("ai-log-inference")
		logger.LogAIInferenceToFile(source, input, output)
		_ = store.LogAIInference(0, source)
	}()
}

func (g *AIClient) marshalTasksForAI(tasks []store.ConsolidatedMessage) string {
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
	// Why: stable id ordering keeps the serialized task block byte-identical across
	// re-scans of the same room, so the user-prompt prefix stays prompt-cache eligible.
	sort.Slice(ctxTasks, func(i, j int) bool { return ctxTasks[i].ID < ctxTasks[j].ID })
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

func (g *AIClient) getValidLang(lang string) string {
	if lang == "" {
		return "Korean"
	}
	return lang
}

func (g *AIClient) getAnalyzeSysInst(analyzer core.SourceAnalyzer, data core.ExtractionContext) string {
	if analyzer != nil {
		return analyzer.GetSystemInstruction(data)
	}
	return `Extract tasks as JSON array: [{"id", "state", "task", "requester", "assignee", "assigned_at", "source_ts", "deadline", "category"}]`
}

func (g *AIClient) getAnalyzeUserPrompt(analyzer core.SourceAnalyzer, data core.ExtractionContext) string {
	if analyzer != nil {
		return analyzer.GetUserPrompt(data)
	}
	return data.MessagePayload
}

func (g *AIClient) parseAnalyzeResults(raw string, currentUserID store.UserID, userEmail string) ([]store.TodoItem, error) {
	logger.Debugf("[AI] raw response: %s", raw)
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
func (g *AIClient) CallGenericAPI(ctx context.Context, email, step, source, modelName, system, prompt string, thinking ThinkingMode) (string, error) {
	if g == nil || g.transport == nil {
		return "", fmt.Errorf("AI client is not initialized")
	}

	req := LLMRequest{
		Model:       modelName,
		System:      system,
		User:        prompt,
		Temperature: 0.1,
		Thinking:    thinking,
	}

	start := time.Now()
	resp, err := g.transport.Generate(ctx, req, 30*time.Second, 2)
	if err != nil {
		return "", err
	}

	_ = trace.Step(ctx, g.tracePrefix+"-"+step, "", int(time.Since(start).Milliseconds()), 0)
	logTokenUsage(ctx, email, step, modelName, source, 0, resp.Usage)
	return resp.Text, nil
}

// logTokenUsage records prompt/completion/reasoning tokens against a (step, model, source,
// report_id) bucket so downstream dashboards can attribute cost. Pass source="" for steps
// that aren't bound to a specific channel and reportID=0 for steps not bound to a report.
// CachedTokens is surfaced in the APM step but not yet persisted (token_usage cached_tokens
// column is a tracked follow-up).
func logTokenUsage(ctx context.Context, email, step, model, source string, reportID store.ReportID, usage LLMUsage) {
	if err := store.AddTokenUsage(email, step, model, source, reportID, usage.PromptTokens, usage.CompletionTokens, usage.ReasoningTokens, usage.CachedTokens); err != nil {
		logger.Warnf("[TOKEN-USAGE] %s/%s: %v", email, step, err)
	}
	// Why: reasoning tokens passed as value so WhaTap MXQL can query thinking-token consumption numerically.
	_ = trace.Step(ctx, fmt.Sprintf("TokenUsage-%s (Prompt: %d, Comp: %d, Think: %d, Cached: %d)",
		step, usage.PromptTokens, usage.CompletionTokens, usage.ReasoningTokens, usage.CachedTokens), "", 0, usage.ReasoningTokens)
}
