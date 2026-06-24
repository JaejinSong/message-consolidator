package ai

import (
	"context"
	"time"
)

// ThinkingMode is a provider-neutral toggle for model "thinking"/reasoning.
// Why: Gemini distinguishes three states — unset (model default), explicitly
// disabled (budget 0), and enabled (budget > 0). Collapsing "unset" into
// "disabled" changes extraction behavior on the Gemini fallback path, so
// ThinkDefault preserves the "leave ThinkingConfig nil" semantics. DeepSeek
// encodes thinking in the model id (chat vs reasoner) and ignores this field.
type ThinkingMode int

const (
	ThinkDefault ThinkingMode = iota // provider default (Gemini: no ThinkingConfig)
	ThinkOff                         // explicitly disable (Gemini: ThinkingBudget 0)
	ThinkOn                          // enable (Gemini: ThinkingBudget = geminiThinkingBudget)
)

// LLMRequest is the provider-neutral generation request shared by every AI step.
// System/User mirror the OpenAI role split; geminiTransport maps System onto
// SystemInstruction and User onto the content payload.
type LLMRequest struct {
	Model       string
	System      string
	User        string
	Temperature float64
	MaxTokens   int
	JSONMode    bool
	Thinking    ThinkingMode
}

// LLMUsage normalizes token accounting across providers. ReasoningTokens maps
// Gemini ThoughtsTokenCount / DeepSeek completion_tokens_details.reasoning_tokens.
// CachedTokens maps prompt-cache hits (DeepSeek prompt_tokens_details.cached_tokens);
// captured but not yet persisted — see the token_usage cached_tokens follow-up.
type LLMUsage struct {
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	CachedTokens     int
}

// LLMResponse is the provider-neutral generation result. FinishReason is
// normalized to "" (normal) | "length" (output truncated — Gemini MAX_TOKENS /
// OpenAI "length") so callers can trigger truncation repair provider-agnostically.
type LLMResponse struct {
	Text         string
	FinishReason string
	Usage        LLMUsage
}

// LLMTransport is the single provider seam: one method, implemented by
// geminiTransport and deepseekTransport. All prompt building, response parsing
// and token logging lives above it in AIClient so both providers share it.
type LLMTransport interface {
	Generate(ctx context.Context, req LLMRequest, timeout time.Duration, maxRetries int) (LLMResponse, error)
}
