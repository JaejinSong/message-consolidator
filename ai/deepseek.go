package ai

import (
	"context"
	"fmt"
	"math/rand"
	"message-consolidator/internal/whataphttpx"
	"message-consolidator/logger"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// DeepSeek model ids and tiers.
//
// NOTE: `deepseek-chat` (non-thinking) and `deepseek-reasoner` (thinking) are
// aliases scheduled for deprecation on 2026-07-24 in favor of `deepseek-v4-flash`
// plus a request-level thinking parameter. Migrating to the parameter form is a
// tracked follow-up; until then the model id is the thinking control surface.
const (
	deepSeekDefaultBaseURL = "https://api.deepseek.com/v1"
	deepSeekChatModel      = "deepseek-chat"     // v4-flash tier, thinking off
	deepSeekReasonerModel  = "deepseek-reasoner" // v4-flash tier, thinking on
	deepSeekProModel       = "deepseek-v4-pro"   // pro tier, thinking on
)

// deepseekTransport implements LLMTransport over the OpenAI-compatible DeepSeek API.
type deepseekTransport struct {
	client *openai.Client
}

func newDeepSeekTransport(apiKey, baseURL string) (*deepseekTransport, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY is not set")
	}
	if baseURL == "" {
		baseURL = deepSeekDefaultBaseURL
	}

	logger.Infof("[DEEPSEEK] Initializing transport (Key length: %d, Prefix: %s..., BaseURL: %s)", len(apiKey), apiKey[:min(4, len(apiKey))], baseURL)

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	// Why: route every DeepSeek call through the WhaTap RoundTripper so outbound
	// HTTP surfaces as an APM step (CLAUDE.md "그 외 → whataphttpx.Client()").
	cfg.HTTPClient = whataphttpx.Client()
	return &deepseekTransport{client: openai.NewClientWithConfig(cfg)}, nil
}

// Generate maps the neutral LLMRequest onto an OpenAI-compatible chat completion.
// Thinking is controlled by the model id (chat vs reasoner), so req.Thinking is
// advisory here and intentionally unused.
func (t *deepseekTransport) Generate(ctx context.Context, req LLMRequest, timeout time.Duration, maxRetries int) (LLMResponse, error) {
	messages := make([]openai.ChatCompletionMessage, 0, 2)
	if req.System != "" {
		messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleSystem, Content: req.System})
	}
	user := req.User
	if user == "" {
		// Why: a chat completion requires at least one non-empty message; mirror the
		// geminiTransport placeholder so system-only prompts stay valid.
		user = "."
	}
	messages = append(messages, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: user})

	apiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: float32(req.Temperature),
	}
	if req.MaxTokens > 0 {
		apiReq.MaxTokens = req.MaxTokens
	}
	if req.JSONMode {
		// Why: DeepSeek supports response_format json_object only (no json_schema).
		apiReq.ResponseFormat = &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject}
	}

	resp, err := t.createWithRetry(ctx, apiReq, timeout, maxRetries)
	if err != nil {
		return LLMResponse{}, err
	}
	if len(resp.Choices) == 0 {
		return LLMResponse{}, fmt.Errorf("empty response from DeepSeek")
	}

	choice := resp.Choices[0]
	out := LLMResponse{Text: choice.Message.Content, Usage: deepSeekUsage(resp.Usage)}
	if choice.FinishReason == openai.FinishReasonLength {
		out.FinishReason = "length"
	}
	return out, nil
}

func (t *deepseekTransport) createWithRetry(ctx context.Context, req openai.ChatCompletionRequest, timeout time.Duration, maxRetries int) (openai.ChatCompletionResponse, error) {
	var resp openai.ChatCompletionResponse
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		apiCtx, cancel := context.WithTimeout(ctx, timeout)
		resp, err = t.client.CreateChatCompletion(apiCtx, req)
		cancel()

		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return openai.ChatCompletionResponse{}, ctx.Err()
		}

		logger.Warnf("[DEEPSEEK] API call failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
		if attempt < maxRetries {
			backoff := time.Duration(1<<attempt) * time.Second
			jitter := time.Duration(float64(backoff) * (0.5 + 0.5*rand.Float64())) //nolint:gosec // Jitter does not need cryptographic strength; math/rand suffices.
			time.Sleep(jitter + 1*time.Second)
		}
	}
	return openai.ChatCompletionResponse{}, fmt.Errorf("all %d attempts failed, last error: %w", maxRetries+1, err)
}

func deepSeekUsage(u openai.Usage) LLMUsage {
	out := LLMUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens}
	if u.CompletionTokensDetails != nil {
		out.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
	}
	if u.PromptTokensDetails != nil {
		out.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	return out
}
