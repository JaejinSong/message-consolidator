package ai

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"
)

var apiKeyPattern = regexp.MustCompile(`(key=)[^&"'\s]+`)

func maskAPIKey(err error) string {
	if err == nil {
		return ""
	}
	return apiKeyPattern.ReplaceAllString(err.Error(), "${1}***")
}

var relaxedSafetySettings = []*genai.SafetySetting{
	{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
	{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
	{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdBlockNone},
	{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdBlockNone},
}

// geminiThinkingBudget caps thinking when ThinkOn. Preview models ignore the cap
// (they think freely regardless), so the exact value only matters for non-preview
// models; it mirrors the prior AnalyzeWithContext budget for continuity.
const geminiThinkingBudget = 3072

// geminiTransport implements LLMTransport over the genai SDK.
type geminiTransport struct {
	client *genai.Client
}

func newGeminiTransport(ctx context.Context, apiKey string, cfgOpts ...func(*genai.ClientConfig)) (*geminiTransport, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not set")
	}

	logger.Infof("[GEMINI] Initializing transport (Key length: %d, Prefix: %s...)", len(apiKey), apiKey[:4])

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
	return &geminiTransport{client: client}, nil
}

// Generate maps the neutral LLMRequest onto a genai GenerateContent call,
// preserving the prior buildConfig/thinking/safety behavior exactly.
func (t *geminiTransport) Generate(ctx context.Context, req LLMRequest, timeout time.Duration, maxRetries int) (LLMResponse, error) {
	cfg := &genai.GenerateContentConfig{SafetySettings: relaxedSafetySettings}
	if req.Temperature != 0 {
		cfg.Temperature = genai.Ptr(float32(req.Temperature))
	}
	if req.MaxTokens > 0 && req.MaxTokens <= math.MaxInt32 {
		cfg.MaxOutputTokens = int32(req.MaxTokens)
	}
	if req.JSONMode {
		cfg.ResponseMIMEType = "application/json"
	}
	if req.System != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.System, "")
	}
	switch req.Thinking {
	case ThinkOff:
		cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(int32(0))}
	case ThinkOn:
		cfg.ThinkingConfig = &genai.ThinkingConfig{ThinkingBudget: genai.Ptr(int32(geminiThinkingBudget))}
	case ThinkDefault:
		// Why: leave ThinkingConfig nil so the model applies its own default.
	}

	user := req.User
	if user == "" {
		// Why: genai rejects an empty Part as an uninitialized oneof field (INVALID_ARGUMENT).
		user = "."
	}

	resp, err := t.generateWithRetry(ctx, req.Model, genai.Text(user), cfg, timeout, maxRetries)
	if err != nil {
		return LLMResponse{}, err
	}
	text, err := extractResponseText(resp)
	if err != nil {
		return LLMResponse{}, err
	}
	out := LLMResponse{Text: text, Usage: geminiUsage(resp.UsageMetadata)}
	if len(resp.Candidates) > 0 && resp.Candidates[0].FinishReason == genai.FinishReasonMaxTokens {
		out.FinishReason = "length"
	}
	return out, nil
}

func geminiUsage(m *genai.GenerateContentResponseUsageMetadata) LLMUsage {
	if m == nil {
		return LLMUsage{}
	}
	return LLMUsage{
		PromptTokens:     int(m.PromptTokenCount),
		CompletionTokens: int(m.CandidatesTokenCount),
		ReasoningTokens:  int(m.ThoughtsTokenCount),
		CachedTokens:     int(m.CachedContentTokenCount),
	}
}

// Why: Safely retries AI API calls with exponential backoff to handle transient errors and rate limits gracefully, ensuring reliability under high load.
func (t *geminiTransport) generateWithRetry(ctx context.Context, modelName string, contents []*genai.Content, cfg *genai.GenerateContentConfig, timeout time.Duration, maxRetries int) (*genai.GenerateContentResponse, error) {
	var resp *genai.GenerateContentResponse
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		apiCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err = t.client.Models.GenerateContent(apiCtx, modelName, contents, cfg)
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
